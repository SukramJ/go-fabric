# Phase 0 — register reconciliation, wave W3 (cluster, schema, parity, conformance, surface)

- Scope: every finding ID of `../2026-11/wave-3-clu-{sec,core,gen,a1,a2,b1,b2}.md`, `wave-3-verdict.md` and the W3 parts of `SUMMARY.md` — 58 findings (1 high, 14 medium, 27 low, 16 note).
- Baseline: go-fabric `95e03096` (branch `review/2026-11-13`). Read-only; no code changed.
- Pins: matter.js `db021da0` and connectedhomeip `6170af84`, read from the `git archive` exports under the session scratchpad (`pin/matter.js`, `pin/connectedhomeip`). The 2026-11 register cites matter.js at `862ad75f`; every matter.js line below was re-located at `db021da0`.
- Method: status read from the code at the baseline, never from the register. "holds" = the fix is in the code path and a named test guards it. Tests named in the tables were run (`go test -count=1 -run <names>` per package) and passed unless stated. MEASURED = read in code or produced by a command; INFERRED is marked.
- Result: 43 recorded fixes hold, 0 regressed; of 15 recorded open, 13 are still true and 2 no longer true; the W3 register has no struck finding. 9 new findings (0 high, 0 medium).

## 1. `wave-3-clu-sec.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W3-clusec-01 | high | fixed (#359) | holds | `cluster/core/access_control.go:646-660` (`writeFabric`: `request && fabric == 0` → UnsupportedAccess; the entries' FabricIndex is consulted only without a request context), call sites `:680` (ACL), `:908` (Extension); the IM path always stamps the context: `bridge/receive_dispatch.go:439-441`, groups `bridge/group_messaging.go:339,350`; no non-test caller of `AccessControl.SetCurrentFabric` (grep). Tests `access_control_test.go:1033` (`…NoAccessingFabricIsUnsupportedAccess`, incl. a set legacy hook and `ReplaceACL` count 0), `:1004`. chip `src/app/WriteHandler.cpp:803-811`, matter.js `AccessControl.ts:364` at the pins. | none — but see R2-P0-acl-B01 (sibling attribute) and R2-P0-test-B09 (no IM-level guard) |
| W3-clusec-02 | medium | fixed (#359) | holds | `operational_credentials.go:2067-2130`: armed (`:2082-2086`), `nocInvoked` → ConstraintError, MissingCsr without CSR, `rootSet` → in-band MissingCsr (`:2111-2113`), session fabric ≠ fail-safe fabric → ConstraintError (`:2120-2125`). Test `operational_credentials_parity_test.go:630` (`TestParityOpcreds_UpdateNOCFailSafeFabricAndRoot`). matter.js `OperationalCredentialsServer.ts:337, :355` at `db021da0`. | none (nil-port behaviour: R2-P0-err-B02) |
| W3-clusec-03 | medium | fixed (#359, owner: adopt chip) | holds | `operational_credentials.go:537-546` (`failSafeRequired` over `FailSafeArmedFor`), gates CSRRequest `:1543`, AddNOC `:1686`, AddTrustedRootCertificate `:2404`; port `general_commissioning.go:1080-1101`; wiring `node/root.go:196`. Test `operational_credentials_parity_test.go:591`. chip `OperationalCredentialsCluster.cpp:341, :472, :760`, `FailSafeContext.h:76-79` at the pin. Row `by_design.md:124, :5497`; marker `OperationalCredentialsServer.ts:602-604` still reads so at `db021da0`. | none |
| W3-clusec-04 | low | fixed (#365) | holds | `operational_credentials.go:1756-1760` (strict whenever the CSR named a session); comment/test `operational_credentials_test.go:323`. matter.js `:240` at the pin. | none |
| W3-clusec-05 | low | fixed (#365) | holds | `general_commissioning.go:668-675` (no ValueOutsideRange; cumulative clamp follows). Test `general_commissioning_test.go:293`. | none |
| W3-clusec-06 | low | fixed (#365) | holds | `cluster/spec/icdmanagement/definition_gen.go:219-220, :267-268` (`DecodeBytes`, unbounded), `script/clustergen/relaxed.go:13-21`. Test `icd_management_test.go:427`. | none |
| W3-clusec-07 | low | fixed (#365) | holds | `operational_credentials.go:1697-1706` (ConstraintError). Test `operational_credentials_test.go:307`. | none |
| W3-clusec-08 | low | fixed (row corrected) | holds | `by_design.md:2022-2038` now says AccessControlEntryChanged names the CASE subject; code `access_control.go:1183-1190`. matter.js `AccessControlServer.ts:275-284` at the pin (`rootNodeId`). No test (a register sentence). | none |
| W3-clusec-09 | note | open | **no longer true** | `cluster/wire/admincommissioning.go:435-438` (OpenCommissioningWindow) and `:559-561` (basic): a session without a fabric answers PAKEParameterError, not Busy. matter.js `AdministratorCommissioningServer.ts:164-177` at `db021da0` (`#adminFabric` throws `PakeParameterError`), chip `AdministratorCommissioningLogic.cpp:52, :91`. Tests `admincommissioning_pase_reject_test.go:55`, `admincommissioning_basic_test.go:65-69`. | STRIKE — both references and the code now agree (changed by the pin lift to `db021da0`) |
| W3-clusec-10 | note | open | still true | `operational_credentials.go:1370-1376` (AttestationRequest), `:1582-1586` (CSRRequest), `vid_verification.go:207-211`: without a session challenge in the context the signature uses the process-wide `attestationChalleng`, set at every PASE establishment (`node/security.go:653`). | FIX |

## 2. `wave-3-clu-core.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W3-clucore-01 | medium | fixed (#365) | holds | `bridged_device_basic_information.go:469-471` (`CheckValue` → `refusedWrite`). Test `bridged_device_basic_information_test.go:158`. | none |
| W3-clucore-02 | medium | fixed (#365) | holds | `identify.go:244-269` (write calls the host handler `:258-262`), countdown end calls it with 0 (`:376-378`). Test `identify_handler_parity_matterjs_test.go:52`. matter.js `IdentifyServer.ts:58` at the pin. | none (race note: R2-P0-conc-B07) |
| W3-clucore-03 | medium | fixed (#365) | holds | `scenes_management.go:702-705` (ConstraintError, UTF-16 units as matter.js `constraint.ts:201` counts). Test `scenes_name_parity_test.go:20`. chip `ScenesManagementCluster.cpp:535-539`; row `by_design.md:127, :5525`. | none |
| W3-clucore-04 | low | fixed (#365) | holds | `identify.go:465-470` (`boundUint16`). Test `identify_time_bounds_parity_matterjs_test.go:26`. | none |
| W3-clucore-05 | low | fixed (#365) | holds | `identify.go:374-375` (version bump + notify at zero). Test `identify_time_bounds_parity_matterjs_test.go:58`. | none |
| W3-clucore-06 | low | open | still true | `general_diagnostics.go:1088` (`maxIfacesForApple = 4`), `:1096, :1149` (synthetic fallback), `:1158-1165` (all-zero MAC `eth0`). No `by_design` row (grep). | FIX (owner question 1) |
| W3-clucore-07 | low | open | still true | `cluster/wire/scenes_management.go:17-27` (stub, "HM has no scene-management concept"); no non-test reference in the module (grep `wire\.ScenesManagement`). | FIX |
| W3-clucore-08 | low | fixed (#365) | holds | `cluster/wire/field_bounds.go:38-56` (`fieldUint` → ConstraintError / InvalidAction); no `& 0xFF` mask left in `cluster/wire` (grep). Test `field_bounds_parity_matterjs_test.go:22`. | none |
| W3-clucore-09 | low | open | **mostly no longer true** | the generated decoder holds chip's option rules: `cluster/spec/messages/definition_gen.go:150` (`MessageResponseId` `DecodeUintIn(…, 1, …)`), `:151` (Label mandatory, max 32), `:250` (Responses max 4). chip's SpokenMessage / AudioMessage couplings (`messages-server.cpp:293-348`) have no counterpart in the snapshot: `MessageControlBitmap` has five bits, none of them (definition_gen.go:92-96), and the request has no languageCode / messageUri. chip checks no duplicate ids (`:364-385`). No test pins an id of 0 (`messages_test.go:226-254`). | STRIKE, with a guard test and one row sentence |
| W3-clucore-10 | note | fixed (#365) | holds | `basic_information.go:776-783` (arm removed). Test `basic_information_test.go:1407`. | none |
| W3-clucore-11 | note | fixed (#365) | holds | `general_diagnostics.go:984-989` (reportable = RebootCount, UpTime, TotalOperationalHours), `:538-546` (unseeded 0). Test `general_diagnostics_test.go:79`. | none |
| W3-clucore-12 | note | open | still true | `descriptor.go:66-77` (version digest = ServerList + PartsList), `:317-319` (reportable = PartsList). A TagList or DeviceTypeList change moves neither. | FIX |
| W3-clucore-13 | note | open | partly true | the tautological test is gone; `node/served_revision_parity_matterjs_test.go:30` reads revision and FeatureMap off constructed servers. Still missing: `MatterReportable ⊆ MatterAttributes`, every listed attribute answers `MatterRead` (no such test found by grep). | FIX |
| W3-clucore-14 | note | fixed (#365) | holds | `scenes_management.go:459-467`. Test `scenes_name_parity_test.go:43`. | none |

## 3. `wave-3-clu-gen.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W3-clugen-01 | medium | fixed (#365) | holds | `cluster/spec/bound_expr.go:30-140`, `write.go:544-552`. MEASURED: the 84 `Bound{Expr}` lines of the generated definitions use node types `+ - * / ^ . maxOf minOf reference` only, all handled; no unit-valued Expr remains (grep). Test `bound_expr_parity_matterjs_test.go:15`. matter.js `Constraint.ts:15-24, :1029-1089` at the pin. | none (latent generator gap: R2-P0-par-B05) |
| W3-clugen-02 | low | fixed (#365) | holds | `cluster/spec/model.go:108-111`, `write.go:381-382`. Test `cpmax_parity_matterjs_test.go:16`. matter.js `constraint.ts:212-230` at the pin. | none |
| W3-clugen-03 | low | fixed (#365) | holds | `script/clustergen/gen.go:1626-1635` (panic `confError`), `:137` (recovered into a generation error). Test `gen_test.go:283-288`. | none |
| W3-clugen-04 | low | open | still true (latent) | `script/generate_matter_schema.go:542-556`: "first access string any cluster states for the same (id, name)". MEASURED at `db021da0`: the 106 generated rows of `schema/attribute_access_gen.go` equal `effective.access` (script `w3_access_cmp.py`: 0 only-generated, 0 only-effective, 0 value differences). | FIX |
| W3-clugen-05 | low | open | still true (latent) | `schema/writable.go:276-285`, `schema/writable_test.go:91, :121` (skip on empty raw access), `endpoint/dispatcher.go:324`. MEASURED: in the 41 clusters of the hand table no effective-`R` attribute is missing; 533 effective-`R` attributes of the 94 other clusters are outside the gate and left to the server. | FIX |
| W3-clugen-06 | low | open | still true | `script/extract-from-matter-js.ts:113-122` (a failed `git rev-parse` is logged, the snapshot still written; no `git status --porcelain`). | FIX |
| W3-clugen-07 | note | fixed (#365) | holds | `cluster/spec/model.go:175-186`, `script/clustergen/gen.go:1540-1563`. Test `gen_test.go:232-235`. | none |
| W3-clugen-08 | note | fixed (#365, #368) | holds | `TestParityCodeMatchesGeneratedSchema` no longer exists (grep); replaced by `node/served_revision_parity_matterjs_test.go:30`. | none |
| W3-clugen-09 | note | open | still true | `cluster/wire/schedules.go:15-17, :107` (cluster 0x0024, not in the snapshot, no non-test use); row D-9 `by_design.md:951` cites `internal/model/custom/climate/matter.go:190-199`, a path this repository does not have (`ls internal/`). | FIX (owner question 2) |
| W3-clugen-10 | note | open | still true | `conformance/doc.go:4-30` names no engine; the engine is `cluster/spec/conformance.go`. | FIX (doc sentence) |

## 4. `wave-3-clu-b1.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W3-club1-01 | medium | fixed (#365) | holds | `cluster/closure/closurecontrol_server.go:452-463` (InvalidInState outside Moving / WaitingForMotion / Stopped, before the mover). Test `closurecontrol_moveto_state_chip_test.go:24`. chip `ClosureControlCluster.cpp:599-605`; the null gate `:560-562` is recorded as not mirrored (`by_design.md:5560`). | none |
| W3-club1-02 | medium | fixed (#359) | holds | `cluster/threadbr/management.go:296-305`, gate `:359`; wiring `node/failsafe.go:68` (`gc.FailSafeFabric`). Test `management_chip_test.go:368`. chip `ThreadBorderRouterManagementCluster.cpp:212`. | none |
| W3-club1-03 | low | fixed (#365) | holds | `cluster/light/colorcontrol_server.go:783-798`, the five sites `colorcontrol_commands.go:272, :351, :408, :425, :624`; dispatcher defers `endpoint/types.go:773-776`. Test `colorcontrol_scenes_parity_matterjs_test.go:44`. matter.js `ColorControlServer.ts:597, :732, :779, :1079, :1145` at the pin. INFERRED: without a scenes port the dispatcher's every-command rule still applies (`types.go:776` is conditional). | none |
| W3-club1-04 | low | fixed (#365) | holds | `cluster/cover/windowcovering_server.go:145-156`. Test `windowcovering_spec_parity_matterjs_test.go:87`. | none |
| W3-club1-05 | low | fixed (#365) | holds | `closurecontrol_server.go:604-609` (Stop keeps the target), `:797-803` (only arrival clears it). Test `closurecontrol_server_test.go:201`. | none |
| W3-club1-06 | low | fixed (#365) | holds | register row `by_design.md:125`, entry `:5560`; marker `ClosureControlServer.ts:17` unchanged at `db021da0`. | none |
| W3-club1-07 | low | fixed (#365) | holds, one residual site | NaN guard at every `MatterFloatValue` read: `measurement.go:320, :400, :483, :574, :1011, :1157, :1285, :1363, :1653`, `flow.go:85`, `soil.go:146-148`. Test `reading_bounds_parity_matterjs_test.go:28`. Not covered: `ElectricalPowerServer.scaled` (`:1057-1066`). | none — R2-P0-bound-B04 |
| W3-club1-08 | low | fixed (#365) | holds | `measurement.go:620-634` (pressure held to 0..32766), `:1366-1369` (concentration clamp). Same test. | none |
| W3-club1-09 | note | open | still true | consequence still unrecorded (`by_design.md` BD-chip-TlsCertificateManagement-RulesInServer). MEASURED read outcome: a list entry that alone exceeds the hard cap turns the whole attribute into `AttributeStatusIB(RESOURCE_EXHAUSTED)` — `bridge/reply.go:441-446`, cap `:356` = `udp.MaxDatagramSize - 200` = 1848 bytes (`transport/udp/listener.go:39`); a certificate may be 3000 bytes (`cluster/tls/certificates.go:40`). | BY_DESIGN (row amendment) |
| W3-club1-10 | note | open | still true, claim corrected | Go: CONSTRAINT_ERROR before the mover (`closurecontrol_server.go:620-630`). chip does not accept the position either: `SetOverallTargetState` refuses it (`ClosureControlCluster.cpp:435-436`, `IsSupportedOverallTargetStatePositioning` `:222-238`) and `HandleMoveTo` answers FAILURE (`:637`) — after the delegate ran and MainState moved (`:622-635`). | BY_DESIGN (row sentence) |

## 5. `wave-3-clu-b2.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W3-club2-01 | medium | fixed (#365) | holds | `cluster/onoff/server.go:402-410` (`before := st.onOff` ahead of the sink; `!before || !st.onOff` invalidates), `off` alike. Test `onoff_lock_test.go:184` (`TestSinkEchoInvalidatesScenes`). matter.js `OnOffServer.ts:98-99, :120` at the pin. | none |
| W3-club2-02 | medium | fixed (#365) | holds | `cluster/switch/server.go:755`, `cluster/sensing/ambient.go:959`, `cluster/servicearea/servicearea.go:831`. Tests `switch/quiesce_test.go:17`, `sensing/ambient_quiesce_test.go:20`, `servicearea/quiesce_test.go:20`, `bridge/transition_reporting_test.go:122`. matter.js `SwitchServer.ts:379-384` at the pin. | none |
| W3-club2-03 | low | fixed (#365) | holds | `cluster/filter/filter.go:399-407`. Test `reset_time_chip_test.go:21`. | none |
| W3-club2-04 | low | fixed (row) | holds | `by_design.md:280` (BD-Matter-ModeSelect-HostOwnsTheMode). | none |
| W3-club2-05 | low | fixed (#365) | holds | `cluster/valve/valve_server.go:449-455`. Test `write_noop_chip_test.go:18`. The Open / Close bumps were kept by the reviewer's answer (`wave-3-verdict.md`, answer 3), recorded there only. | none |
| W3-club2-06 | note | fixed (row) | holds | same row, `by_design.md:291-294` (null without a source). | none |

## 6. `wave-3-clu-a1.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W3-clua1-01 | medium | fixed (#365) | holds | `cluster/thermo/events.go:245-264`, callers `thermostat_server.go:724, :1155`, `thermostat_typed.go:141`. Test `running_mode_parity_matterjs_test.go:19`. matter.js `ThermostatServer.ts:1186, :1194-1206` at the pin. | none |
| W3-clua1-02 | medium | fixed (#365) | holds | `cluster/thermo/atomic.go:465-491` (`commitAll` reverts the committed attributes, the status stands for each), `:498-526`. Test `atomic_rollback_parity_matterjs_test.go:63`. matter.js test `AtomicWriteHandlerTest.ts:491` at the pin. INFERRED: commit then revert announces Presets twice (`presets.go:373`, `atomic.go:510`) where matter.js's rolled-back transaction announces nothing; the final value is the old one. | none |
| W3-clua1-03 | low | fixed (#365) | holds | `thermostat_server.go:683, :875`. Test `ltne_raise_parity_matterjs_test.go:19`. | none |
| W3-clua1-04 | note | fixed (#365) | holds | `thermostat_server.go:1455` (`int`). Test `ltne_raise_parity_matterjs_test.go:43`. | none |

## 7. `wave-3-clu-a2.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W3-clua2-01 | medium | fixed (#359) | holds | `cluster/jointfabric/administrator.go:222-231`, gates `:480, :520`. Test `administrator_rules_test.go:268`. chip `joint-fabric-administrator-server.cpp:325-326, :369-370`. | none |
| W3-clua2-02 | medium | fixed (#365) | holds for "applied"; "persisted" was never closed | `cluster/levelcontrol/held.go:93-101` (applied unless Bridged or after an OTA boot); the kit sets `Bridged: true` (`device/defaults.go:290-293`). Test `startup_level_parity_matterjs_test.go:19`. matter.js `LevelControlServer.ts:224` at the pin. | none — R2-P0-store-B06 |
| W3-clua2-03 | low | fixed (#365) | holds | `cluster/lock/doorlock_server.go:794-797`. Test `doorlock_commands_test.go:433` (`TestDoorStateUnchangedIsNoChange`). | none |
| W3-clua2-04 | note | open | still true | `cluster/levelcontrol/levelcontrol_server.go:432-446` (`NewServer` → `initEngine(*cfg.Transitions, true)`), `transitions.go:93` (`s.onOff = cfg.OnOff`, no feature test), `:305`, `:436`; `held.go:104-107` gates it. | FIX |

## 8. Dispositions

**W3-clusec-10 — FIX.** Claim: three signatures fall back to a process-wide challenge. Evidence above. matter.js signs with the session only (`OperationalCredentialsServer.ts:113, :148` `certification.sign(session, …)`, `:491` `this.context.session.attestationChallengeKey` at `db021da0`). Fix: drop `attestationChalleng` and `SetAttestationChallenge` (`operational_credentials.go:75, :216-226`; caller `node/security.go:653`); an invoke without `InvokeAttestationChallengeFromContext` answers FAILURE. Proving test: AttestationRequest, CSRRequest and SignVIDVerificationRequest without a stamped challenge fail; with one they verify against it. INFERRED: the PASE path gets the challenge from the bridge's session table (`bridge/receive_dispatch.go:577-579`); whether that holds for a session opened by `OpenFromPaseWithID` was not traced — the fix's own test must show it.

**W3-clucore-06 — FIX (or BY_DESIGN, owner question 1).** matter.js serves the OS interfaces minus the unassigned MAC, operational ones first, at most 8 (`GeneralDiagnosticsServer.ts:506-518` at `db021da0`). Ours caps at 4 and invents `eth0` with an all-zero MAC, on an Apple claim the code states without a source (`general_diagnostics.go:1083-1088`). Fix: cap 8, drop the all-zero fallback (serve `[]`), sort operational first. Test: five qualifying interfaces → five entries; none qualifying → empty list. If the cap stays: row "BD-Matter-GeneralDiagnostics-InterfaceCap — NetworkInterfaces is held to 4 entries and a synthetic `eth0` replaces an empty list; matter.js serves up to 8 and filters the unassigned MAC (`GeneralDiagnosticsServer.ts:506-518`); reason: <the owner's observation, with its date and iOS version>".

**W3-clucore-07 — FIX.** Delete `cluster/wire/scenes_management.go` and its test; `core.ScenesManagement` is the one server (`endpoint/materialize.go` mounts it). CHANGELOG `### Removed` with the migration line. Proof: `go build ./...` and the API-diff guard name exactly the removed identifiers.

**W3-clucore-09 — STRIKE, with a guard.** The claim ("MessageResponseID 0 is queued with SUCCESS") is refuted by the generated decoder (table). Add: one case to `TestMessagesRefusals` (id 0 → CONSTRAINT_ERROR as chip `messages-server.cpp:379-381`; label of 33 → CONSTRAINT_ERROR `:383-385`; missing label → the decoder's status) and one sentence in BD-chip-Messages-CoreHeld naming the decoder as the holder of these rules and SPKM / AUDM as not in the snapshot.

**W3-clucore-12 — FIX.** Fold DeviceTypeList and TagList into the digest of `MatterDataVersion` (`descriptor.go:66-77`) and list DeviceTypeList, ServerList and TagList (with the feature) in `MatterReportable`. Test: `AddTags` after the first read moves the DataVersion. matter.js citation for "every state write versions": unverified at the pin (not re-read).

**W3-clucore-13 — FIX.** Extend `TestServedClusterRevisionAndFeatureMap` (`node/served_revision_parity_matterjs_test.go:30`), which already walks every root server and every kit server: `MatterReportable ⊆ MatterAttributes`, and every id of `MatterAttributes` answers `MatterRead` with ok. This is the standing guard for the class W3-clucore-11 was an instance of.

**W3-clugen-04 — FIX.** `writeAccessFile` reads `Effective.Access` (as `writeTimedFile` and `writeLargeMessageFile` do) instead of the first (id, name) match in snapshot order. Proof: regenerated bytes identical at the pin (measured equal today), plus a generator test with two clusters sharing (id, name) and differing privileges.

**W3-clugen-05 — FIX.** Generate `readOnlyAttributes` from `effective.access.rw` for every snapshot cluster, next to `attribute_access_gen.go`; drop the hand table and the raw-access skip in `writable_test.go`. Proof: generated table ⊇ today's 41 clusters; a dispatcher test that a read-only attribute of a cluster outside today's table answers UNSUPPORTED_WRITE at the gate.

**W3-clugen-06 — FIX.** `process.exit(1)` when `git rev-parse HEAD` fails; refuse a dirty tree (`git status --porcelain`). Proof: the Makefile target fails on a touched element file (manual, or a script test with a temp repo).

**W3-clugen-09 — FIX (owner question 2).** Delete `cluster/wire/schedules.go` (+ test) and row D-9, whose cited path does not exist here; cluster 0x0024 is in neither the snapshot nor a served surface. If kept: rewrite D-9 in English with real paths.

**W3-clugen-10 — FIX.** One sentence in `conformance/doc.go`: the conformance engine is `cluster/spec/conformance.go` / `field_conformance.go`; this package is the conformance *suite*. No rename (API).

**W3-clua2-04 — FIX.** In `NewServer`, clear `Transitions.OnOff` when the features lack OO, as `New` does (`held.go:104-107`), and drop the call-site gate in `device/defaults_ports.go`. matter.js `LevelControlServer.ts:558, :732` at `db021da0`. Test: `NewServer` without OO and with `Transitions.OnOff` — a MoveToLevelWithOnOff does not switch OnOff, and a plain MoveToLevel executes while the OnOff port reads off.

**W3-club1-09 — BY_DESIGN (amend BD-chip-TlsCertificateManagement-RulesInServer).** Add: "Consequence over UDP: a list entry that does not fit one message (a certificate above ~1.8 kB; the limit is 3000 bytes) makes the read of the attribute answer RESOURCE_EXHAUSTED (`bridge/reply.go` `chunkReportData`); connectedhomeip avoids it by encoding ids only without a large-payload transport (`TLSCertificateManagementCluster.cpp:136-166` at `6170af84`). Kept by the owner decision of 2026-10-28." The chip lines `:136-166` were not re-read at the pin in this phase (unverified here; cited from the row).

**W3-club1-10 — BY_DESIGN (one sentence in BD-chip-ClosureControl-RulesInServer).** "A TargetPosition whose feature is not advertised is CONSTRAINT_ERROR before the mover, from the enum value's conformance; connectedhomeip answers FAILURE after the delegate ran (`ClosureControlCluster.cpp:435-436, :622-637` at `6170af84`)."

## 9. New findings

**R2-P0-acl-B01 — low (class of W3-clusec-01).** `cluster/core/group_key_management.go:465-472`: a GroupKeyMap write whose request has no accessing fabric falls back to `g.currentFabric` and then proceeds with fabric 0 (`:493-518`: `ListGroupKeyMappings(ctx, 0)`, `SetGroupKeyMapping{FabricIndex: 0}`); the commands of the same server refuse fabric 0 (`:603-616`). Reachable over PASE before AddNOC (ACL bypassed, `im/write.go`). chip `WriteHandler.cpp:803-811`, matter.js `AccessControl.ts:364` answer UNSUPPORTED_ACCESS. Impact bounded: `SetCurrentFabric` has no non-test caller (grep), so the write lands under fabric 0, where no key set can exist; whether the SQLite schema refuses `fabric_index 0` is unverified. MEASURED: of the six fabric-scoped writable attributes of the snapshot (Binding, Acl, Extension, DefaultOtaProviders, GroupKeyMap, GroupcastAdoption) this is the one served attribute that does not refuse (Binding `binding.go:343-345`, OTA `ota_software_update_requestor.go:532-535`; GroupcastAdoption is not served). Fix: `LookupFabricFilter`, `request && fabric == 0` → UnsupportedAccess; test as `access_control_test.go:1033`.

**R2-P0-test-B09 — low.** No Interaction-Model-level guard for the high finding: `im/write.go` has no "fabric-scoped list without accessing fabric" gate (grep), and no bridge / im test writes ACL over a fabric-less session; each server refuses on its own (B01 is the one that did not). Fix: the gate in `im.HandleWriteRequest` from the schema's fabric-scoped flag (chip puts it in WriteHandler, matter.js in `authorizeWrite`), plus one table test over the snapshot's fabric-scoped writable attributes.

**R2-P0-err-B02 — low.** `operational_credentials.go:537-546`: with a nil fail-safe port `failSafeRequired` is false, and UpdateNOC skips both checks (`:2082-2086, :2120-2125`) — the four NOC commands run without any fail-safe. The other consumers read a nil port as disarmed (`general_commissioning.go:1095-1098`, `threadbr/management.go:300-302`, `jointfabric/administrator.go:226-228`). The node wires it (`node/root.go:196`); a host composing the server itself fails open, silently. Fix: nil = disarmed; tests wire a fake port.

**R2-P0-api-B03 — note.** Legacy and fabric-blind hooks remain public: `SetCurrentFabric` on AccessControl (`access_control.go:390`), OperationalCredentials (`:2500`, used as fallback `:2136-2140`), GroupKeyManagement (`:697`, fallbacks `:415, :471, :606`), GeneralCommissioning (`:1059`); `IsFailSafeArmed func() bool` on both NetworkCommissioning configs (`network_commissioning_wifi.go:93-96`, `_thread.go:86-88`), used only by tests (`network_commissioning_wifi_test.go:54`, `_thread_test.go:120`, `_wifi_persist_test.go:31`). Fix: remove them with the migration line; tests use the request context and `FailSafe`.

**R2-P0-bound-B04 — low.** `cluster/measurement/measurement.go:1057-1066` (`scaled`, Voltage / ActiveCurrent / Frequency) passes NaN to `clampToInt64` (`:1070-1078`), whose comparisons are false for NaN, ending in `int64(NaN)` — the implementation-defined conversion W3-club1-07 removed elsewhere. Fix: NaN → null; extend `TestReadingBoundsParity`.

**R2-P0-par-B05 — low (latent).** `script/clustergen/gen.go:1513-1518` renders any bound it does not recognise as an opaque `Bound{Expr}`; `bound_expr.go:56-76` parses an unknown node to an empty expression, `evalBound` reports no number and the bound admits every value. W3-clugen-03 made the same degradation fail for conformance; bounds still degrade. Nothing degrades at `db021da0` (measured, table). Fix: the generator fails on a node type `parseBoundNode` does not know (one shared list), test as `gen_test.go:283-288`.

**R2-P0-store-B06 — low.** The kit's held LevelControl takes no persisted start-up state: `device/defaults.go:293` builds `HeldConfig` without `StartUpCurrentLevel` / `OnLevel` / `Options`, while OnOff receives `Initial` (`:255`). A controller's StartUpCurrentLevel write (`levelcontrol_server.go:621-629`) is gone after a restart; the attribute is `RW VM`, quality `X N` (`level-control.element.ts:67-68` at the pin). Under the Aggregator the value is never applied, so the effect is the read-back. Unverified: whether an `Initial` entry for LevelControl is consumed elsewhere for this path (`defaults_ports.go:126-129` consumes Options / OnLevel for the port server only). Fix or `by_design` row — phase 2 (persistence) should decide for the class of quality-N attributes.

**R2-P0-conc-B07 — note (inferred, not exercised).** `cluster/core/identify.go:366-379`: after the countdown's CompareAndSwap to 0 the goroutine calls the host with 0; a write or command landing in between (`:258-264`: host called with t, then stored) leaves the host stopped while IdentifyTime counts from t. Fix: serialise the host calls of the countdown and the request paths.

**R2-P0-doc-B08 — note.** Stale statements: `bridge/receive_dispatch.go:428-436` ("falls back to fabric=1 (last resort)"); `cluster/core/access_control.go:662-669` ("Extension (0x0001) is not implemented") above the Extension branch at `:899`; `by_design.md:951` (D-9, dead path, see W3-clugen-09).

## 10. Not checked / not performable

- No test was written for the new findings (no code edits in this phase); B01, B02, B04 are read from the code, B07 is inferred.
- R2-P0-acl-B01: whether the production SQLite schema refuses `fabric_index 0` — the current DDL of `matter_group_key_map` was not located (only the pre-upgrade DDL in `store/groupkeys_upgrade_test.go:77-85`).
- The 250 citation drifts (`862ad75f` in code comments against the pin `db021da0`) are on their ratchet and were not re-read line by line; only the citations a finding rests on were re-located (all held).
- chip `TLSCertificateManagementCluster.cpp:136-166` (W3-club1-09) and the matter.js `DescriptorServer` / `Datasource` lines of W3-clucore-12: not re-read at the pin.
- W3-clua1-02's double announcement and W3-club1-03's no-scenes-port fallback: inferred from the code, not exercised.
- `make race`, fuzz, full test runs: out of this reviewer's brief. Targeted runs only (all `ok`): `cluster/core`, `cluster/threadbr`, `cluster/jointfabric`, `cluster/closure`, `cluster/light`, `cluster/cover`, `cluster/measurement`, `cluster/onoff`, `cluster/switch`, `cluster/sensing`, `cluster/servicearea`, `cluster/filter`, `cluster/thermo`, `cluster/spec`, `cluster/wire`, `cluster/valve`, `cluster/lock`, `cluster/levelcontrol`, `node`, each with `-run` of the tests named above.
- Scripts used for the two measurements: `w3_access_cmp.py`, `w3_ro_cmp.py` in the session scratchpad (not committed).

## 11. Owner questions (to be put as a wizard)

1. W3-clucore-06: mirror matter.js (8 interfaces, no synthetic entry) or keep the Apple cap as a `by_design` row with its observation? Recommended: mirror — matter.js is the Apple-hardened reference and serves 8.
2. W3-clugen-09 / W3-clucore-07: delete the dormant `wire.SchedulesServer` and the `wire.ScenesManagement` stub now (API removal, migration line), or deprecate for one window first?
3. R2-P0-test-B09: the fabric-scoped-list gate in `im/` (one place, from the schema) in addition to the servers' own refusals? Recommended: yes — it is where both references put it.
4. R2-P0-store-B06: persistence of controller-written quality-N attributes on kit-built servers — module duty (as labels and scenes are) or host duty with a `by_design` row?
