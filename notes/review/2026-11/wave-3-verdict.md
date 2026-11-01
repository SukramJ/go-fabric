# W3 verdict (2026-11-01, HEAD 769ea3c2) — final

Theme files (all done): `wave-3-clu-sec.md`, `wave-3-clu-core.md`, `wave-3-clu-gen.md`, `wave-3-clu-b1.md`, `wave-3-clu-b2.md`, `wave-3-clu-a1.md`, `wave-3-clu-a2.md`.
Model note: clu-sec, clu-core and clu-gen ran on Fable (launched before the owner's model rule of 2026-11-01); b1, b2 and a run on Opus with Sonnet maps (`notes/reads/read-review-w3-cluster-{a,b}.md`).

## Verified against the pins (Fable re-read, both sides)

| ID | Severity | Verdict |
| --- | --- | --- |
| W3-clusec-01 | **high** | confirmed: `cluster/core/access_control.go:676-681` (ACL) and `:924-929` (Extension) scope a write without an accessing fabric to `entries[0].FabricIndex` or to fabric 1; chip `WriteHandler.cpp:807-811` at 6170af84 answers UnsupportedAccess for a fabric-scoped list write without an accessing fabric; `binding.go:343-345` already does so here. Reachable over PASE before AddNOC and over session 0 (W2-wire-01). Fix: refuse `request && fabric == 0` with UnsupportedAccess, drop both fallbacks, flip `access_control_test.go:979-1010`. |
| W3-clusec-02 | medium | confirmed: `operational_credentials.go:1967-1990` checks only "armed"; matter.js `OperationalCredentialsServer.ts:355-360` requires session fabric == fail-safe fabric (ConstraintError) and `:337-345` MissingCsr after AddTrustedRootCertificate. |
| W3-clusec-03 | medium | owner decision pending (ahead of matter.js: chip refuses CSR/AddNOC/AddTrustedRoot from a fabric other than the fail-safe's with FailsafeRequired; matter.js shares our gap). |
| W3-clugen-01 | medium | confirmed: `cluster/spec/write.go:470-476` resolves only dotted references; `maxOf`/arithmetic bounds return `(0,false)` and admit every value. 23 generated definition files carry `Bound{Expr}` at the pin; the two controller-writable cases are guarded by hand in their servers. matter.js evaluates them (`Constraint.ts:156-211, 969-1040`). Fix: evaluate the expression tree in `bound()` (maxOf/minOf/+/-/unit scaling) against peers. |
| W3-club2-01 | medium | confirmed: `cluster/onoff/server.go:381-383` stores the sink-echoed value into `st.onOff` before `on`/`off` (`:398`, `:421`) test for a change, so a synchronous host echo skips `invalidateScenes`; matter.js tests the state at command start (`OnOffServer.ts:98-100, 119-122`). The by_design row at `by_design.md:2535-2556` does not record this consequence. Fix: capture the pre-command state before calling the sink. |
| W3-club2-02 | medium | confirmed: `cluster/switch/server.go:181-183` (debounce, long-press, multi-press timers) and `cluster/sensing/ambient.go` (hold-time, `Close` at `:948` has no caller) implement no `MatterQuiesce`; `bridge/bridge.go:1898-1904` stops timers only through it. matter.js `SwitchServer.ts:379-384` stops on dispose. Fix: implement `MatterQuiesce` on Switch, AmbientContext, ServiceArea. |
| W3-club1-01 | medium | confirmed: `cluster/closure/closurecontrol_server.go:315-358` sets MainState=Moving without a state check; chip `ClosureControlCluster.cpp` at 6170af84 (MoveTo handler, ~:561 null current state, ~:600-605 MainState ∉ {Moving, WaitingForMotion, Stopped}) answers InvalidInState. matter.js has no ClosureControl server logic (chip is the authority; needs an "Ahead of matter.js" row, W3-club1-06). |
| W3-club1-02 | medium | confirmed: `cluster/threadbr/management.go:291-296, 349` asks the node's fail-safe "armed by anyone" (`node/failsafe.go:54` passes `gc.FailSafeArmed`); chip `ThreadBorderRouterManagementCluster.cpp:212` requires `IsFailSafeArmed(accessingFabric)`. Same class as W3-clusec-03 (fabric-blind fail-safe) — one decision, one fix: a fail-safe port that takes the accessing fabric. |
| W3-clucore-01 | medium | confirmed: `bridged_device_basic_information.go:461-463` returns a plain error (→ FAILURE); `basic_information.go:815-820` returns `refusedWrite` with the spec status (ConstraintError). Fix: route through `CheckValue`/`refusedWrite` like the root cluster. |
| W3-clucore-02 | medium | confirmed: `identify.go:212-224` (MatterWrite) only starts the countdown; `:233-247` (MatterInvoke) is the only caller of the host handler. matter.js reacts to `identifyTime$Changed` from both (`IdentifyServer.ts:57-58`). Fix: call the host handler from the write path too. |
| W3-clucore-03 | medium | confirmed: `scenes_management.go:698-700` truncates SceneName to 16 and answers SUCCESS; chip `ScenesManagementCluster.cpp:536-539` at 6170af84 answers ConstraintError. Fix: reject with ConstraintError; correct the decoder comment at `bridge/fields_reader_scenes.go:15-17`. |
| W3-clua1-01 | medium | confirmed: matter.js `ThermostatServer.ts:1194-1206` sets ThermostatRunningMode from SystemMode (Off/Heat/Cool) when served; `cluster/thermo/thermostat_server.go:1093-1123` (write) and `:682-698` (SetSystemMode) leave it untouched. Fix: mirror the coupling in both paths, emit RunningModeChange. |
| W3-clua1-02 | medium | confirmed: `cluster/thermo/atomic.go:443-444` commits attribute by attribute; a later host-port refusal leaves the earlier commit applied. matter.js `AtomicWriteHandler.ts:314-315` rolls back the transaction when any status is not Success. Fix: precommit all, then commit all, and revert the already-committed attributes on a later failure (or commit through one host call). |
| W3-clua2-01 | medium | confirmed: `cluster/jointfabric/administrator.go:469, 505-513` test `armed` from `gc.FailSafeArmed` (any fabric); chip `joint-fabric-administrator-server.cpp:325-326, 369-373` requires the calling fabric's fail-safe. Third instance of the fabric-blind fail-safe class (with W3-clusec-03, W3-club1-02). |
| W3-clua2-02 | medium | confirmed: `cluster/levelcontrol/levelcontrol_server.go:329-335` documents "never applies StartUpCurrentLevel" with the Aggregator reasoning; matter.js `LevelControlServer.ts:224` skips only endpoints owned by an Aggregator, a native light (device kit) applies the 0/null/value rule. Fix: apply at start-up when the endpoint is not bridged (needs persistence of the level — plausible gap). |

## Counts (final, from the reviewers' reports)

| Theme | high | medium | low | note |
| --- | --- | --- | --- | --- |
| clu-sec (Fable) | 1 | 2 | 5 | 2 |
| clu-core (Fable) | 0 | 3 | 6 | 5 |
| clu-gen (Fable) | 0 | 1 | 5 | 4 |
| clu-b1 (Opus) | 0 | 2 | 6 | 2 |
| clu-b2 (Opus) | 0 | 2 | 3 | 1 |
| clu-a1 (Opus) | 0 | 2 | 1 | 1 |
| clu-a2 (Opus) | 0 | 2 | 1 | 1 |
| **W3** | **1** | **14** | **27** | **16** |

All 15 high/medium findings re-read by Fable against both sides: 14 confirmed, 1 owner decision (W3-clusec-03, with W3-club1-02 and W3-clua2-01 one class: fabric-blind fail-safe port).

## Owner decisions pending (W3)

- Fabric-aware fail-safe port (chip authority; matter.js shares the gap in OpCreds): adopt chip → one port change (`node/failsafe.go:54`) feeding OpCreds CSR/AddNOC/AddTrustedRoot, Thread BR, Joint Fabric.
- "Ahead of matter.js" register rows for ClosureControl (W3-club1-06) and Joint Fabric where chip is the only authority — ClosureControl done in the W3 PR (`BD-chip-ClosureControl-RulesInServer`, d284b510); Joint Fabric open.

## Fixed in PR (W3, branch `review/w3`)

The W3 fix PR's items (`notes/briefs/review-2026-11-w3.md`); item 11
(ThreadNetworkCommissioning fail-safe) and W3-clusec-04..08 are the
owner's reviewer's and stay as above. Notes left open are listed below.

| ID | Commit | Change |
| --- | --- | --- |
| W3-clugen-01 | 2433875c | `cluster/spec/bound_expr.go`: expression bounds computed (+ − × ÷ ^, maxOf/minOf, dotted references); unit bounds restated in the type's units by the generator; boolcfg hand guard dropped |
| W3-clucore-01 | c0b1d66a | BDBI NodeLabel over 32 bytes: ConstraintError via `CheckValue` / `refusedWrite` |
| W3-clucore-02 | 32135158 | IdentifyTime write runs the host's IdentifyHandler; the countdown's end calls it with 0 |
| W3-clucore-03 | 5a053733 | AddScene: SceneName over 16 is ConstraintError; decoder comment corrected; `BD-chip-ScenesManagement-SceneNameLength` |
| W3-club2-01 | 9313ce35 | OnOff On/Off invalidate on the state at command start; by_design row corrected |
| W3-club2-02 | 06875546 | `MatterQuiesce` on Switch, AmbientContextSensing, ServiceArea; `cluster.Quieter.Stop` |
| W3-club1-01 | d284b510 | ClosureControl MoveTo: InvalidInState outside Moving / WaitingForMotion / Stopped |
| W3-club1-06 | d284b510 | Ahead row `BD-chip-ClosureControl-RulesInServer` (marker `ClosureControlServer.ts:17`) |
| W3-clua1-01 | b31dfd65 | SystemMode Off/Heat/Cool sets ThermostatRunningMode, RunningModeChange emitted |
| W3-clua1-02 | 6e2a8727 | Atomic CommitWrite reverts committed attributes when a later host port refuses; the status stands for every attribute |
| W3-clua2-02 | 243781c0, 54d5a590 | Held LevelControl applies `HeldConfig.StartUpCurrentLevel` unless `Bridged` or after an OTA boot |
| W3-clucore-04, -05, -08, -11, -14 | 36a635dd, 54d5a590 | IdentifyTime / wire decoder fields bounded (ConstraintError / InvalidAction); countdown zero reported; GeneralDiagnostics reportables and RebootCount 0; FabricSceneInfo without `Fabrics` |
| W3-clugen-02, -03, -07, -08 | 102c0770 | `cpMax`; unknown conformance fails generation; `spec.UnresolvedDefault`; revision table reads the constants |
| W3-club1-03, -04, -05, -07, -08 | 58aa47df | ColorControl scene invalidation; GoTo percentage without PA; ClosureControl target kept; NaN null; Pressure / concentration within Min/Max |
| W3-club2-03, -04, -05, -06 | 42aaec0d | ResetCondition stamps LastChangedTime; DefaultOpenDuration no-op; `BD-Matter-ModeSelect-HostOwnsTheMode` |
| W3-clua1-03, -04, W3-clua2-03 | 67688d49 | LocalTemperature null under LTNE; SetpointRaiseLower in int; DoorState only on change |

Not fixed here: W3-clucore-10 (`cluster/core/basic_information.go` is W1's;
the dead LocalConfigDisabled arm stays for the W1 PR or a follow-up). Notes
W3-clusec-09/10, clucore-06/07/09/12/13, clugen-04/05/06/09/10, club1-09/10,
clua2-04 stay open.
