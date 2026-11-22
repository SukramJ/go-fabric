# Pin lifts — what moved upstream, and what it means here

One section per lift of the matter.js pin (`parity/schema.json` →
`matter.sourceCommit`). Every upstream fix between the two commits is
classified against this module: **have it** (by construction or already
ported), **gap** (a register entry in `matter_behaviour_findings.md`, closed by
a later wave), **not applicable** (JS-runtime concern, harness, a role or
feature this module does not have yet — with the milestone that will have
it), **caught up** (matter.js now implements behaviour this module already
had ahead of it — a row of the *Ahead of matter.js* register in
[`by_design.md`](./by_design.md); the two implementations are reconciled onto
matter.js and the row closes, see `CLAUDE.md` workflow step 5), or **open**
(to be settled by the test port named). Independently of the commit list,
every lift re-reads each marker of that register at the new pin: an upstream
change can close a gap without a commit message that says so. The trigger
is the check over those markers: `script/aheadcheck` (`make ahead-check`)
reads each row's marker file and text at the pin and at a ref, and the
weekly `ahead-markers` job in `.github/workflows/matterjs-pin.yml` runs it
against matter.js `origin/main` and goes red with the finding "caught up?"
naming the row as soon as a marker text is gone there — between lifts, not
only at one. The manual re-read at the lift is the confirmation: a row gets
the verdict **caught up** only after the new pin has been read and the
behaviour compared, never on the check's finding alone. Since review II's
phase 4 a lift has two further steps. The list of matter.js's certification
cases is held to the pin: `internal/chiptool:TestCertifiabilityDocument`
fails on a case file of `support/chip-testing/test/cert` at the snapshot's
pin that the Go-TH leg neither runs nor classifies, so a lift regenerates
`internal/chiptool/testdata/matterjs-cert-cases.json` (`go test
./internal/chiptool/ -update-matterjs-cert-cases`, against the matter.js
checkout at the new pin) and classifies or ports what the list gained. And
the snapshot carries each cluster's `attributeOrder` and the
`globalAttributeOrder`, matter.js's `Scope.membersOf` order, which every
report follows (`im.CompareAttributeIDs`): a lift reads that part of the
snapshot's diff as it reads the model's, a moved order being a change on
the wire, and the extractor stops when a cluster's order would depend on a
feature selection the snapshot does not carry. Owner's work (Fable
row); the reading map behind each section is a Sonnet `read-*.md` under
`notes/reference/`, verified line by line before anything landed here.

## db021da0 → b25ed960 (2026-11-19, review II after phase 7)

118 commits; 37 skipped (10 `[CHIP BUILD]`, 8 version bumps, 15 dependency
bumps, 4 `ci:`), 81 classified below. Map:
`notes/reference/read-matterjs-delta-b25ed960.md`. The verdicts are three
Fable fact notes by area (protocol, sessions and security; the device side;
controller, commissioning, OTA and harness) and three on the register's
markers, matter.js read at b25ed960. connectedhomeip was not read: its
neighbour checkout stood at 8df2e483, not the harness commit 6170af84, so
every chip statement in this section is unverified. Model: the snapshot's
only semantic change is UnitLocalization TemperatureUnit's `constraint: "in
supportedTemperatureUnits"`
(`packages/model/src/standard/elements/unit-localization.element.ts:24-27`
at b25ed960, a local override of matter.js, #4901); besides it the element
files carry matter.js's new `isSecret` quality (#4931, log redaction, a
matter.js extension the specification does not have), which the extractor
now reads so that the snapshot moves once in this lift (decision 304).
`thermostat.element.ts`, cited for the Thermostat device type, was no file at
`db021da0` either; the device type is `thermostat-device.element.ts`, the
cluster `thermostat-cluster.element.ts`, and the two citations now say so.
Decisions: 287 (the last lift before the release: one pin PR, every gap
closed in its own wave P1…P5), 288, 289, the owner's 290–296 and the
coordinator's 297–306 (`notes/review/2026-11-13/decisions.md`). The units
P1-A … P5-B named below are the lift's wave plan; each closes its own
findings (`matter_behaviour_findings.md`, section "matter.js pin b25ed960").

The register's markers at b25ed960 (`make ahead-check`, then the re-read):
**caught up** — `BD-chip-Thermostat-Schedules` (MSCH, implemented behind
matter.js's forward feature "thermostat-msch"; P3-A; reconciled in P3-A, row open by decision 291), `BD-chip-Thermostat-Suggestions`
(TSUGGEST, not gated; P1-E; closed in P2b-F), `BD-chip-Thermostat-Events` (TEVT, behind
"thermostat-events"; P4-A — not flagged by the check: its marker line
survives the implementation), `BD-chip-ZoneManagement-RulesInServer` (P3-B,
the zone ID by decision 290; closed in P3-B), `BD-chip-Commissioner-CsrNonceEchoed` (P2-D),
`BD-chip-EnergyReporting-FakeReadings` (matter.js's test node answers the
three triggers; the reference daemon's values and endpoint bits adopted in
P3-E, decision 303 — closed in P3-E) and `BD-chip-ScenesManagement-SceneNameLength`
(`addScene` refuses a name over 16 UTF-8 bytes; P2-A — not flagged either:
its marker text still occurs further down the file; closed in P2b-A); outside the *Ahead*
rows `BD-Matter-Scenes-RemainingCapacity` (#4963, P2-A; closed in P2b-A). By decision 291 the
MSCH and TEVT rows stay open after their reconcile as "available ahead of
matter.js's forward gate", the gate lines their new markers. **Partly caught
up** — `BD-chip-Commissioner-AbortDisarmsFailSafe` (every step failure now
ends the fail-safe; the abort signal is still missing in matter.js; new
markers in the pin PR; P3-D; the mirrored part reconciled in P3-D, the row open for the abort) and `BD-Matter-OTAProvider-InProgressPerFabric`
(the in-progress map left the provider for the manager's attempts; the cap
of 16 per fabric and the fabric-removal drop stay ours; P4-C). **False
alarm**, new marker texts only — `BD-Matter-Camera-FabricRemovalEndsStreams`
(logging added to the requestor), `BD-Matter-OwedFabricEvents` (the fabric
load renamed for the label cut), `BD-Matter-OTA-AttestedIdentityKept` (the
peer-detail check refactored, still no attested pair),
`BD-chip-SessionEvictionFabricFirst` (the helper took a set argument, still
no fabric filter). `BD-chip-UnitLocalization-TemperatureUnitInSupported`:
the write path is caught up by the generic `in` check and the entry retires;
the start-up difference is decision 289's hybrid, extended to the two
sibling servers by 296 (P1-F). Rows whose marker is a line that survives the
implementation it stands against (the Events and SceneNameLength misses)
are listed and re-read in the pin PR. CASE responder: the resume order of
`BD-Matter-CASEServer-ResumedSessionBeforeSigma2Resume` now mirrors
matter.js; `BD-Matter-CASEServer-CloseOnUnfinishedEstablishment` narrows to
the unacknowledged Success of a full handshake, which decision 288 adopts
(P1-A).

Found at the work-in-progress commit and settled in the pin PR. `make
cite-check`: 17 hard errors (citations past the end of a shrunken file, the
OTA provider among them; `test/cert/unit-testing-cluster.ts` deleted)
re-cited from a Fable re-read, behaviour that changed pinned `at db021da0`;
1239 labels `at db021da0` relabelled `at b25ed960` by script where every
file they bind is byte-identical between the two commits (decision 309);
drift 304 → 441, off-pin 124 → 2889. The matter.js test register: every
moved blob re-checked and the 28 new files classified (20 planned for a
unit, 8 excluded), counters planned 2 → 22, ported 162 → 146, missing
918 → 1218, left 630 → 714 (decision 310). Certification cases: the range
adds one, TC-OPCREDS-3.3 (#4874), whose DUT is the controller; not run
(decision 305): step 11 needs the controller's UpdateNOC (P5-B), and steps
5b, 7b and 9-11 read field values from the TH log, which the daemon does
not print. `goCertReadAt` moves to b25ed960; DD-3.5 moves with it, the
five cases whose matter.js content changed stay read at db021da0
(decision 307) until P2b-F re-mirrors them at b25ed960. Three register rows caught up before their units land are
listed in `script/aheadcheck/caught-up.txt` (decision 308).

Wave P3c (2026-11-21) settled three matters read at b25ed960 that no PR row
above carries: `dropHandshakeNotBegun` (`bridge/securechannel.go`) closed as
matching — the comment and its citations re-read at the pin, the standalone
ack following from the exchange's ack timer (P3c-A); the bridge's peer loss
on a give-up of an exchange it opened, now only while the exchange heard
nothing (`MessageExchange.ts:490`, `:674-681`; F-b25ed960-51, fixed in
P3c-A); and the PASE session cleared inside a CommissioningComplete answered
OK (`FailsafeContext.ts:156-157`; F-b25ed960-53, fixed in P3c-F, decision
410). Opened for P4-D: F-b25ed960-52 and -54.

Wave P4 (2026-11-21) closed the protocol rows opened for it and re-read two
citation sets at the pin. P4-D1: the bridge's peer loss by registration
order (F-b25ed960-49; `ExchangeManager.ts:623`, `SessionManager.ts:1266-1284`;
`BD-Matter-PeerLoss-OwnSessionAtTheSameInstant` retired, decision 417), the
liveness kick on the bridge's BDX exchanges in either role (F-b25ed960-50;
`MessageExchange.ts:593-646`), the receive timeout's peer loss
(F-b25ed960-52; `:882-892`) and the BDX retransmission cap of 7.4 s, found
and closed in the unit (F-b25ed960-55; `:70-78`, `:1313-1318`). P4-D2: a
fail-safe rolled back by a command over CASE closes the PASE sessions inside
the command (F-b25ed960-54, Medium by measurement; `FailsafeContext.ts:283`,
`Session.ts:336-342`); the revoke's residuals stay open (F-b25ed960-56).
P4-S: `bridge/conn.go`'s eight ExchangeManager.ts citations and the 42 `DAV:`
lines of `controller/attestation` re-read at b25ed960, the package doc naming
`validate` and the `validateCollecting` it wraps; ExchangeManager's
`#sessions.sessions` → `registeredSessions` is the SessionManager set rename
(same membership), not a behaviour change. Opened for P4b: F-b25ed960-57
(an ApplyUpdateRequest after `RemoveConsent` passes).

Wave P5 (2026-11-22) closed the last two PR rows of the lift and one protocol
residual. P5-A (decision 292): the update queue's phases, the failed consent
with its 24-hour expiry, `#retarget`, the abortable announcement, the consent
tables and the exact offer (#4826; F-b25ed960-21, -C3, -C4 closed); the
deferred queue run, the announcement's log classification and the legacy
status view are `by_design` rows (`BD-Matter-OTAManager-*`). P5-A2: the update
check and QueryImage select stored images through one helper as
`findApplicable` (`DclOtaUpdateService.ts:394-407`; decision 482). P5-B
(decision 293): the controller rotates a commissioned node's NOC (#4874;
F-b25ed960-20 closed), its outcome a value (*Ahead of matter.js* row
`BD-Matter-CertificateUpdate-TypedOutcome`, owner decision W2). P5-C: a revoke
of the commissioning window sends the CloseSession (F-b25ed960-56 (ii); (iv)
and (v) stay open). P5-S: `images.go` and `interaction.go` citations re-read
at b25ed960. No marker caught up; opened: F-b25ed960-C5 (the NOC update's
force-close cause, not exported).

### Protocol, sessions, security

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4882 (e3551a69d) | a subscription follows each newer CASE session of its subscriber (same peer, equal CATs); a session is establishing until its handshake completes; per-peer eviction at establishment over established responder sessions; peer loss cut by registration order (`SessionManager.ts:757-808`, `ServerSubscription.ts:819-834`, at b25ed960) | **gap (medium) → implemented (P2b-B)** for the supersession, the requeue of a failed update on a left session and `sessionsRegisteredSince` (F-b25ed960-1 closed; decisions 350-352): the device table fires `SetOnSessionSuperseded` per older CASE session a newly established one supersedes, the subscriptions and their routes move (`im/subscription.Manager.MoveSession`, `bridge.Bridge.SessionSuperseded`, wired by `node`), a report that failed on the session left is resent at once uncounted, `client/exchange.Manager.SessionsRegisteredSince` and the reboot armer by registration order; residuals F-b25ed960-40 … -43, `BD-Matter-Subscription-KeepAliveNotDeferred`, `BD-Matter-Subscription-PrimingMovesWithItsSession`; controller side **closed in P1-A**: the responder's establishing state, the Success complete at its first transmission (onTransmitted), the `completeEstablishmentOf` refusal, the eviction at establishment, peer loss by registration order (the exchange's cutoff, `handlePeerShutdown`'s keep) and the StartUp keep (`client/exchange`, `client/handshake`, `controller/client`; decision 311); device side **closed in P2-J** (F-b25ed960-23, decision 312): the device's CASE responder installs its session establishing and establishes it at its Success's first transmission, a resumed one at the initiator's Success, awaited with a deadline; hidden from the pickers, `Sessions()`, the peer-presence check and the per-peer cap, which runs at establishment over established sessions (`secure/operational`, `bridge`, `node/security.go`); the peer-set adoption at the Success's first transmission **closed in P2-J** (F-b25ed960-35, decision 335); the bridge's peer loss on an exchange it opened by registration order **closed in P4-D1** (F-b25ed960-49, decision 405; `secure/operational` `Manager.ClosePeerLostBefore`); a SubscribeResponse send failure closing the subscription **have it** | `im/subscription/manager.go:652-698` binds a subscription to one session for life; `bridge/subscribe.go:1187-1213`; `secure/operational/manager.go:1636-1687` (before P2-J); `client/exchange/manager.go:1479-1496` cuts by creation time |
| #4908 (abfc6bc34) | controller: a closing BLE channel force-closes the unsecured session while PASE runs, so the pending read fails at once (`ControllerCommissioner.ts:482-551`, at b25ed960) | **gap (low) → closed in P3-D** (F-b25ed960-7) — mirrored for the Channel path: PASE ends at once on the channel's loss — the channel's exchange manager is closed where matter.js force-closes the unsecured session (`ControllerCommissioner.ts:482-483`, at b25ed960); the CommissioningConnection reclassification has no counterpart, a Channel has no candidate race | `controller/commissioner/commissioner.go` `establishOverChannel`; before P3-D no watch on `ch.Closed()` during `handshake.PASE`, the read ended at the PASE timeout |
| #4922 (a72412954) | one BLE-loss watch per PASE session for its lifetime; a channel closed before `pair()` returned fails the attempt (`ControllerCommissioner.ts:495-518`, at b25ed960) | **partly (low) → closed in P3-D** — a session whose channel closed while PASE completed is refused, the lifetime watch was already in place (`BleChannel.isClosed` is the Go `Closed()` channel); the BleError reclassification has no counterpart; a step that starts after the loss gets the channel's close as its cause (`flow.channelLoss`), this module's addition (Fable post-read of P3-D, F1; `BD-chip-Commissioner-AbortDisarmsFailSafe`) | `controller/commissioner/commissioner.go` `establishOverChannel`, `flow.go` `channelLoss`; before P3-D a channel closed before PASE returned yielded a session closed a moment later |
| #4930 (ca9a4e584) | sessions bound to a replaced Fabric object close when the fabric goes | **have it** (by construction: sessions are selected by fabric index) | `secure/operational/manager.go:1450-1488`; `bridge/bridge.go:1946-1969` |
| #4933 (d796d23b7) | the exchange created for an unmatched message takes the inverse of its I flag (`MessageExchange.ts:218-243`, at b25ed960) | **have it** | `bridge/ackpump.go:236-258`; `client/exchange/manager.go:1345-1359` |
| #4935 (3f4cf97d8) | the ack-only exchange of an unsolicited message is closed also when the ack send throws | **have it** (by construction: the ack goes out outside any exchange) | `client/exchange/manager.go:1139-1142`, `:1344-1359`; `bridge/ackpump.go:236-258` |
| #4936 (33b077c66) | the I flag in message log lines | **not applicable** (log only) | — |
| #4937 (ac1ef3417) | the transport decision logged once per connection | **not applicable** (log only) | — |
| #4931 (83a06054f) | values of model fields marked `isSecret` are logged as "(redacted)" (`SecretRedaction.ts:13-35`, at b25ed960); 40 fields across 17 element files | **partly (low) → closed in P4-B** (owner decision W1, 433: unconditional, no switch, `BD-Matter-SecretRedaction-NoOffSwitch`) — the mark is generated onto `spec.Field` and `spec.Attribute` from the snapshot's `isSecret` (41 marks), `spec.Redact`, `RedactEntry` and `ContainsSecret` mirror `SecretRedaction`, and the two debug lines that print read values (the read and the subscription priming report) redact them; before, the module logged no command fields or written values and redacted its own secret API types, but had no model-driven redaction | `controller/client/redact.go:14-60`; `bridge/receive_dispatch.go:405-417`, `bridge/subscribe_dispatch.go:271-287`; the snapshot carries `isSecret` since this lift |
| #4943 (8fd1fc992) | controller: an unacknowledged StatusResponse to a report closes the subscription so its owner replaces it (`PeerSubscription.ts:68-120`, at b25ed960) | **gap (low) → implemented (P2b-E, 593140d2)** (F-b25ed960-8 closed); a difference that predates the unit stays: Go sends the final status synchronously, so `isReading` stays true and `ResetTimer` waits for its MRP budget, where matter.js clears both first and awaits the close after (`ClientSubscriptionHandler.ts:147-150`) — liveness is judged up to one MRP budget late | `client/interaction/handler.go:121-158`, `subscriptions.go:406-447`, `subscribe.go:341`, `interaction.go:267-300` |
| #4947 (d2d5d6b77) | controller: the subscription timeout timer stops when the last peer subscription leaves (`ClientSubscriptions.ts:172-187`, at b25ed960) | **gap (low) → implemented (P2b-E, ad9f690d)** (F-b25ed960-9 closed) | `client/interaction/subscriptions.go:205-220` |
| #4944 (b84c5d1bc) | mDNS traffic counts logged once per minute | **not applicable** (log only) | — |
| #4945 (fbcd1738c) | the standard crypto fallback logged at info | **not applicable** (JS runtime) | — |
| #4953 (1f3a0999c) | the JS runtime stays active until the mDNS service closed, so goodbyes go out on shutdown | **not applicable** (JS runtime lifecycle) | `mdns/advertiser.go:27-39` withdraws with goodbyes; whether Stop flushes them before the socket closes: unverified |
| #4897 (c8f44861d) | lint: block bodies in two Key setters | **not applicable** (no behaviour change) | — |
| #4995 (e4326a175) | string lengths count UTF-8 bytes, not UTF-16 code units: constraint validation, TLV string bounds, fabric label, group and scene name; interface name cut at 32 bytes (`constraint.ts:203-213`, `TlvString.ts:69-100`, at b25ed960) | **gap (medium) → implemented (P2b-A)** (decision 294; F-b25ed960-3 closed) — UTF-8 bytes in `spec.StringLength`, `tlv.Element.RawLength`, AddGroup and AddScene; stored scene names, the root NodeLabel, the UserLabel lists and nonvolatile strings cut at load, the generic node path logging no values (the Go model carries no `isSecret`) and storing a cut value at the restore's end where matter.js stores it with the next write; `cpMax` stays codepoints (`constraint.ts:217-235`); fabric label and interface name counted bytes already (`BD-chip-FabricLabelBytes` kept) | `cluster/spec/write.go:367-375`, `:777-785`; `cluster/core/groups.go:272-281`; `cluster/core/scenes_management.go:259-263`, `:1066-1076`; `tlv/utf8.go:72-82` |
| #5016 (74a6d45de) | the commissioning window closes after 20 failed attempts in it: a PASE pairing that fails before its session exists, or a fail-safe armed during the window that ends without commissioning (`DeviceCommissioner.ts:44`, `:160-199`, at b25ed960) | **gap (medium) → mirrored (P3-C), one residual** (decisions 299, 374, 375, 386; F-b25ed960-4 closed) — both halves counted per window, cap 20 closes the window; the timed lockout stays ahead (`BD-Matter-PASE-TimedLockout`, now also reached through fail-safe ends); residual: an attempt abandoned by closing the PASE session counts only when its fail-safe runs out (open finding "A PASE session closed by the peer does not expire the fail-safe") | `bridge/securechannel.go` `recordFailedAttempt`, `bridge/pase_close.go` `failSafeSessionsClosed` / `trackFailSafeAttempt`, `bridge/commissioning_window.go` `generation`, `cluster/core/general_commissioning.go` `SetFailSafeAttemptTracker`; before P3-C only PASE failures counted (`recordPaseFailure`), with no caller on a fail-safe's end |
| #5011 (8addfb158) | SuppressResponse: the commands run and no InvokeResponse goes out, also when a CommandDataIB was produced (`InteractionServer.ts:1104-1109`, at b25ed960) | **gap (low) → closed in P1-C** (decision 298; F-b25ed960-6): `bridge/receive_dispatch.go` sends no InvokeResponse under SuppressResponse, also with a CommandDataIB (`InteractionServer.ts:1104-1109`, at b25ed960) | before P1-C `bridge/receive_dispatch.go:862-868` still sent a response that carries a CommandDataIB, matter.js's rule at db021da0 |
| #5012 (b25ed9604) | an enum value Matter does not define in a command request is CONSTRAINT_ERROR, also behind another validation error; other request errors still only warn; matter.js's own exemptions (`packages/node/src/behavior/state/validation/ValueValidator.ts:158-185`, `ProtocolService.ts:674-704`, at b25ed960) | **gap (medium) → closed in P1-B and P2-H** (decisions 298, 319, 339) — P1-B for the servers held to their definition: the gate collects every error (nameScope.errs) and decides as `ProtocolService.ts:671-705` does — a command's Refusal answers errors[0], else an undefined enum value is CONSTRAINT_ERROR, else a warning; the undefined-value check moved to the datatype check, a member-less enum accepts every value, a nil request is judged as {}; the exemption set is matter.js's (DoorLock's 15 Refusals and the Relaxed get*Schedule indexes, CertificateChainRequest's Refusal, SetRegulatoryConfig.NewRegulatoryConfig and TriggerEffect's fields Relaxed; `RequestCheck.Unchecked` deprecated); Identify runs an undefined EffectVariant as Default. P2-H: the gate is now central — `endpoint.TopologyDispatcher` runs `spec.AdmitRequest` before each server's `MatterInvoke` for every server a wire command reaches, a hand-read request judged in its generated form (`spec.HandForm`; decision 339, F-b25ed960-29 closed); absent fields are read as `{}` (decision 334, F-b25ed960-31 closed, `BD-Matter-Invoke-AbsentFieldsAsEmptyStruct`); an absent response is judged as `{}` in the dispatcher (`spec.CheckResponseAsWritten`), for every server with a registered definition, a hand-written cluster/core response as the bridge's response writer writes it (decisions 340, 343; F-b25ed960-30 closed). Open: a local invoke validated by state (F-b25ed960-36) | before P1-B `cluster/spec/request_check.go:30-41`, `:133-158` stops at the first error and refuses only with a stated Refusal; `ErrUnknownEnumValue` exists (`cluster/spec/field_conformance.go:808-810`) |

### Device side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4867 (200fb61ac) | a held list entry proxy is bound to its entry, not its index; the Thermostat presets fallback cached | **not applicable** (managed-state proxies, JS runtime) | `cluster/thermo/thermostat_server.go:984-985` copies presets on read |
| #4901 (be36b7d1d) | the localization servers start with a supported value; a TemperatureUnit outside SupportedTemperatureUnits is CONSTRAINT_ERROR (`UnitLocalizationServer.ts:17-38`, `constraint.ts:157-168`, at b25ed960); client: a constraint-breaking write is forwarded, a fabric-scoped list write sends only our entries | device: the write path **caught up** (entry retired, decision 289), the start-up **closed in P1-F** (289's hybrid for the three servers, the siblings by 296: an unconfigured start as matter.js, a configured contradictory value refused, `BD-Matter-Localization-ConfiguredStartValueRefused`; `BD-chip-UnitLocalization-TemperatureUnitInSupported` retired, `unitSink`'s duplicate check removed; TimeFormatLocalization's unset calendar F-b25ed960-25, decision 315, closed in P2b-I: pointers, `NoFeatures`, decisions 358-360; decision 360's node option `node.Config.TimeFormat` in P3b-H, decision 400); client: be36b7d1d's forwarded writes ported on the device's answer (P1-F); client: **divergence exists** (`BD-Matter-ClientNode-WriteNotThroughCache`, its text in the pin PR), forwarding have it, the fabric-scoped entry selection backlog | `cluster/core/unit_localization.go:54-74`; `cluster/core/localization_configuration.go:67-77`; `cluster/core/time_format_localization.go:66-74`; `controller/client/interact.go:257-275` |
| #4979 (355798964) | Presets write: handle, scenario, name support, then the total count, the per-scenario count, then the duplicate name (`ThermostatServer.ts:1850-1975`, at b25ed960) | **gap (medium) → closed in P1-E** — the order mirrored (`cluster/thermo/presets.go` `validatePresetWrite`), the cases ported; the status differed on the wire (TC_TSTAT_4_2 run 2 step 9, named by the commit) | `cluster/thermo/presets.go:163-246` checks the total count first and the duplicate name before the per-scenario count |
| #4467 (28fff8231) | MatterScheduleConfiguration, behind forward feature "thermostat-msch" (`ThermostatServer.ts:267-271`, at b25ed960) | **caught up** (`BD-chip-Thermostat-Schedules`) **→ adopted (P3-A)** — write rules, normalisation, commit checks in three passes, ScheduleTypes start checks mirrored; configured schedules and `SetSchedules` under the content rules (O-6, decision 376), the host's replace exempt from the lifecycle rules (O-8, decision 378, `BD-Thermostat-HostSchedules`); our off-by-default gate kept (decision 291) — **reconciled, row open (291)**; F-b25ed960-46 and -47 open; the commit's raise of NumberOfPresets over stored presets (`#limitCoveringStored`, `ThermostatServer.ts:393-399`, `:299-304`) **closed in P1-E** (`cluster/thermo/presets.go` `limitCoveringStored`) | `cluster/thermo/schedules_write.go` (`validateSchedules`, `resolveBuiltIn`, `validateTransitions`, `validateScheduleCommit`, `writeSchedules`); `schedules.go` (`seedSchedules`, `assertScheduleTypes`, `SetSchedules`); `atomic.go` (`commitWrite`, `precommitAtomic`, `atomicChanged`) |
| #4988 (ba725846b) | the scenes of a removed fabric leave the Scene Table at removal and at start; remaining capacities recomputed | **have it** | `cluster/core/scenes_management.go:517-548`, `:728-731`; `node/fabric_state.go:72-135` |
| #4990 (520c4491d) | the Thermostat events (TEVT), behind forward feature "thermostat-events" (`ThermostatServer.ts:272-276`, `:2086-2259`, at b25ed960) | **caught up** (`BD-chip-Thermostat-Events`) **→ reconciled in P4-A** — the five differences (OccupancyChange never emitted, SetpointChange.Occupancy, ActivePresetChange, ActiveScheduleChange, LocalTemperatureChange threshold and interval) adopted; D6: every event on the command's endpoint or the stamped one; D8: the start's events queued to the first SetEndpoint, the reconcile clear kept (matter.js clears, `ThermostatBehaviorTest.ts:134-149`), its events queued (inferred, no matter.js test); D9: the citations moved from connectedhomeip to matter.js; per-event enabling not mirrored (owner decision W2, 434); the gate kept (decision 291) | `cluster/thermo/events.go:55-161`; `cluster/thermo/presets.go:575-578`; `cluster/thermo/schedules.go:277-284` |
| #4993 (a4df212a5) | ThermostatSuggestions (TSUGGEST), not forward-gated; a setpoint moved only to keep the limits or the deadband keeps the active preset (`ThermostatServer.ts:764-789`, at b25ed960) | **caught up** (`BD-chip-Thermostat-Suggestions`), **reconciled in P1-E and P2-G, the row closes in P2b-F** (coordinator decision C-9) — P1-E mirrored five items (UniqueID counter, override tracking, unsynchronised time, suggestions of a removed preset, timer) and the reconciled-setpoint rule (a gap with Presets alone, closed); P2-G settled the stored list and override (`ThermostatConfig.SuggestionStore`), the application's write (`SetAttributes`) and its activation of a preset (`ActivatePreset`) (decisions 316-318, F-b25ed960-26 … -28 closed); **the row closed in P2b-F** (decision 336): `thermo.Ahead` drops the key, the device layer accepts the feature without an extension row and the reference bridge serves TSUGGEST by default; the remaining differences are `BD-Matter-Thermostat-Suggestions-Seams` (the whole-second timer by C-10) | `cluster/thermo/suggestions.go:382-467`; `cluster/thermo/thermostat_server.go:1356-1372` |
| #4840 (e653b55f4) | the ZoneManagement server with the rules (`ZoneManagementServer.ts:67-251`, at b25ed960) | **caught up** (`BD-chip-ZoneManagement-RulesInServer`) **→ closed in P3-B** — `ZoneManagementServer.ts:67-251` carries the command rules, `TwoDCartesianGeometry.ts:24-60` the polygon check (at b25ed960); reconciled in P3-B: `camera.Zones` mirrors both — MaxZones over all zones, the Focus check in an update, the zone ID assigned by the server (decision 290), the last ID host-persisted (O-1, O-7, decisions 371, 377, `BD-Matter-ZoneManagement-LastZoneIdHostPersisted`) — and the test is ported; row and entry retired | `cluster/camera/zone.go` (`createTwoDCartesianZone`, `updateTwoDCartesianZone`, `nextZoneID`), `zone_geometry.go`; before P3-B the port chose the zone ID and a create did not check MaxZones |
| #4997 (31e5a8c41) | the test app stops a simulated zone once its detected duration is exceeded | **not applicable** (chip-testing only) | — |
| #5000 (05645e839) | CoolingWithReheat is the sequence without heating; Auto turns the running mode Off when the sequence forbids the direction | **have it** — the case cannot arise: the sequence is derived as CoolingAndHeating, CoolingOnly or HeatingOnly and a write is ignored; the six new ThermostatBehaviorTest.ts cases are left by design (`BD-Matter-Thermostat-ControlSequenceFromFeatures`, coordinator decision C-3; F-b25ed960-22 closed) | `cluster/thermo/thermostat_server.go:1897-1909`, `:1131-1137` |
| #4956 (9afeee342) | the UnitTesting cluster served on matter.js's all-clusters TH | **not applicable** (chip-testing only) to the module; the reference daemon serves UnitTesting (0xFFF1FC05) on the Aggregator, endpoint 1, behind `--unit-testing` (TESTING ONLY; decisions 337, 354, P2b-F), as matter.js's all-clusters test instance does, so TC-IDM-2.1, 3.1 and 4.1 are re-mirrored at b25ed960 (P2b-F; with TC-DRLK-3.2 and TC-SWTCH-3.2 the five cases decision 307 kept at db021da0) | moves TC-IDM-2.1, 3.1 and 4.1 lines (`goCertReadAt`, pin PR); `examples/reference-bridge/unit_testing.go`, `internal/chiptool/gocert_idm_test.go` |

### Controller side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4921 (a7ac64f19) | the root always composes full-family, whatever its DeviceTypeList; one warning for an empty root DeviceTypeList the peer reported (`ClientStructure.ts:1715-1740`, at b25ed960) | **gap (medium) → closed in P2-C** (decision 297) — the root composes full-family (`compositionOf`), one warning per node and start for a reported root DeviceTypeList that names no numeric device type (`checkRootDeviceTypes`); before, a root with an empty DeviceTypeList got no composition, so no endpoint was placed | `controller/client/structure.go` (`compositionOf`, `checkRootDeviceTypes`); before P2-C `:589-600`, `:1019-1028` |
| #4923 (097e5a8da) | on the node = named by the root's PartsList, or by that of an endpoint on the node once the peer reported its lists; the owner derived on every apply, no stored claims (`ClientStructure.ts:1408-1581`, at b25ed960) | **gap (medium) → closed in P2-C** (decisions 297, 342) — placement from the current lists, no stored claims (`placeParts`, `findEndpointsOnTheNode`, `eraseEndpointsOffTheNode`); under a wildcard the image admits an endpoint that describes itself (decision 342, `BD-Matter-Controller-NodeImageBounded`), so devices reported before the root names their aggregator are placed; an endpoint naming itself is `BD-Matter-Controller-PartsListSelfClaim`; before, a bridge that named its devices only in the aggregator's PartsList showed none | `controller/client/structure.go`, `controller/client/image_bounds.go`; before P2-C `structure.go:641-683`, `:932-1006` |
| #4849 (51cc7cf84) | `PairedNode.reconnect()` awaits re-enabling the subscription | **not applicable** (the JS package's legacy API) | — |
| #4926 (a136e8385) | an ICD's IdleModeDuration is the default ceiling only when it is at least the device ceiling (`PhysicalDeviceProperties.ts:130-140`, at b25ed960) | **gap (medium) → closed in P1-D**: `intervalBounds` takes the IdleModeDuration only when it is at least the device ceiling (`controller/client/properties.go`); wire-visible before: a short IdleModeDuration became the report interval asked for | `controller/client/properties.go:355-382` takes the minimum for every ICD |
| #4927 (d7e3a454f) | test: no event is delivered again when the device drops the subscription and the session stays | **have it** (on the Go client the resubscribe asks above the last event number; the upstream commit is test-only); the test case is ported in P1-D (`controller/client:TestClientEventNotificationEventNumbersParity`) | `controller/client/network.go:347`, `:531` |
| #4833 (a37a502c3) | (a) stored discovery TTLs dropped on load; (b) the TTL no longer multiplied by 1000; (c) a peer that failed to load is found by address and hides no peer after it (`Peers.ts:436`, at b25ed960) | (a) **not applicable** (matter.js's storage migration); (b) **have it**; (c) **closed in P1-D**: `Registry.GetByAddress` and `ForAddress` find a node that failed to load by the peer address in its record (`controller/client/registry.go` storedPeerAddress); `client.Node.PeerAddress()` of such a node and the `CompleteCommissioning` consequence F-b25ed960-24 (decision 314) **closed in P2b-K**: the stored address answered, a fabric operation on the node refused with its load error (decision 357) | `controller/client/registry.go:255-290`, `:685-692` |
| #4836 (c043fdc22) | the SQLite storage driver buffers a transaction's writes | **not applicable** (the Node.js driver); whether `controller/client/sqlstore` has an overlapping-transaction problem of its own: unverified, review backlog | — |
| #4928 (92a99ddf2) | every commissioning step failure ends the fail-safe; the expiry skips a device that stopped responding, awaits a re-arm in flight, checks the response and needs an existing session (`ControllerCommissioningFlow.ts:428-454`, `:1097-1132`, at b25ed960) | **partly → closed in P3-D** (decisions 301, 383; F-b25ed960-17 closed) — `ClientRequest.RequireExistingSession`; the commissioner's `expireFailsafe` in matter.js's order (the channel-drop skip, the re-arm in flight cancelled on a skip and awaited before the send, the response checked, an existing session only); the ahead disarm keeps its by_design row (`BD-chip-Commissioner-AbortDisarmsFailSafe`); the missing-session error stays `ErrNoSession` (decision 301) | `controller/commissioner/flow.go` (`expireFailsafe`, `expiryOwed`, `sendFailsafeExpiry`), `network.go` (`detachArmInterval`, `awaitArmInterval`, `cancelArmInterval`); `client/interaction/provider.go` (`ClientRequest.RequireExistingSession`); before P3-D `flow.go` `resetFailsafeTimer` sent unless the session was closed |
| #4939 (0bbd48704) | a CD vendor_id / product_id_array mismatch with BasicInformation is an error-level finding, also when the device reported neither (DAV:579-595, at b25ed960) | **gap (low) → closed in P2-D** (decision 295) — the CD/BasicInformation VendorID/ProductID mismatch, an unreported value included, is the error finding `CertificationDeclarationFieldMismatch` (`validate.go` checkDeclaration); a policy rejection names the error findings; the attr-status debug log of commissioning reads (CCF:477-482, :776-781) not adopted, log only; before, ours failed hard and compared an unreported value as 0 | `controller/attestation/validate.go`, `controller/commissioner/flow.go`; before P2-D `validate.go:52`, `:357-364` |
| #4940 (65269302a) | a hard attestation failure carries the findings collected before it (DAV:283-293, at b25ed960) | **gap (low) → closed in P2-D** — `attestation.Error.PrecedingFindings`; the flow hands the policy the preceding findings, then the failure (CCF:1332-1336) | `controller/attestation/check.go`, `controller/commissioner/flow.go`; before P2-D `check.go:85-95`, `flow.go:1174-1181` |
| #4938 (05b729727) | the CSRResponse validated in one validator: missing, empty, NOCSRElements over RESP_MAX 900, signature, decode, CSRNonce, CSR; the DAC key required (`NocsrValidator.ts:82-126`, at b25ed960) | **caught up** (`BD-chip-Commissioner-CsrNonceEchoed`), **reconciled in P2-D**, the row retired — `attestation.ValidateNOCSR` (FieldMissing, ResponseEmpty, ElementsTooLong at RESP_MAX 900, AttestationSignatureInvalid, ElementsUnparseable, CsrNonceMismatch, CsrInvalid, in matter.js's order), every failure `ErrCommissioning` (decision 302); AttestationElements over RESP_MAX as the hard failure `AttestationElementsTooLong` the policy judges; the CSR step requires the DAC key, its absence an internal error as matter.js's InternalError (CCF:1450-1452; coordinator decision C-19) | `controller/attestation/nocsr.go` (`ValidateNOCSR`), `controller/commissioner/flow.go` (`certificates`) |
| #4874 (80db76155) | a new controller operation: rotate a commissioned node's NOC (UpdateNOC on one CASE session, CommissioningComplete on a new one), with cert case TC-OPCREDS-3.3 (`OperationalCertificateUpdate.ts:158-243`, at b25ed960) | **gap (medium, a feature) → closed in P5-B** (decision 293; F-b25ed960-20) — `client.Registry.UpdateOperationalCertificate` over `commissioner.Commissioner.UpdateOperationalCertificate`, a mirror of `OperationalCertificateUpdate.update`; the outcome of a failure a value (`BD-Matter-CertificateUpdate-TypedOutcome`, owner decision W2) | `controller/commissioner/noc_update.go`, `controller/client/fabricops.go`; the device side serves it (`cluster/core/operational_credentials.go:2644-2680`) |
| #5003 (f783b219a) | the nodejs-shell's JSON input accepts omitted optional and null nullable struct values | **not applicable** (JS shell) | — |
| #4826 (2ae4f2b99) | the OTA provider and manager rewritten around update attempts the manager owns: query admission, apply decision table, one consent per peer with 24 h failed-update retention, delegation opt-in, candidate order, phases and linking, undeliverable updates ended (`OtaUpdateAttempts.ts:23-181`, at b25ed960) | **gap (medium) → closed in P4b (unit C) and P5-A** (decision 292, 14 differences; option (b), the attempts owned by the manager, decision 435; the consent gap F-b25ed960-57 fixed with it, decision 436; the queue's phases, the failed consent and the offer in P5-A, F-b25ed960-21 closed; the stored-image selection in P5-A2, decision 482); `BD-Matter-OTAProvider-InProgressPerFabric` partly caught up | `cluster/core/ota_provider.go:385-484`, `:601-701`; `controller/softwareupdate/manager.go:811-871`, `:1261-1432`, `:1502-1596` |

### Harness

37 commits of matter.js's own chip-testing runner, adapters and suites
(CHIP's Python and YAML cases run in matter.js's legs, new `app-fast`
suites, TC-BR-4 on the matterjs-server leg). None adds or removes a
`TC-*.test.ts`; three carry library source (#4977, #4963, #4996), four move a
case file the Go-TH leg ports or cites.

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4932 (7597d7ceb) | TC-DD-3.5 checks the CASE stage at session creation | **not applicable**; moves a case Go ports | TC-DD-3.5 `certTest(` 152 → 153, step 13's TH log pattern now `SecureSession +Created secure CASE session`; whether the Go port's evidence for that step is affected: unverified, read in the pin PR (`internal/chiptool/gocert_commissioning_test.go`) |
| #4950 (58f53446d) | clear the chip-local apps' /tmp platform files before each cert case | **not applicable** | matter.js's own chip-testing harness |
| #4949 (d5e53de5e) | name the rejected candidate when an adjacent log run times out | **not applicable** | `tc-support.ts`, cited by the Go-TH leg, re-read in the pin PR |
| #4948 (d9552e184) | write the test app storage one write at a time | **not applicable** | matter.js's own chip-testing harness |
| #4955 (9588a2e4e) | derive the server leg's controller PICS from its certification declaration | **not applicable**; moves cases Go ports | TC-DRLK-3.2 content changed (line 80), TC-SWTCH-3.2 394 → 403 (`goCertReadAt`, `internal/chiptool/gocert_test.go`) |
| #4957 (4fd2c849a) | read the all-clusters app's --enable-key from the run's app-args | **not applicable** | matter.js's own chip-testing harness |
| #4958 (1f8e7982e) | run TC-BR-4 on the matterjs-server controller leg | **not applicable** | matter.js's own chip-testing harness |
| #4960 (9f903435b) | run the multi-run RVCCLEANM tests | **not applicable** | matter.js's own chip-testing harness |
| #4961 (274bec256) | run the multi-run RVCOPSTATE tests | **not applicable** | matter.js's own chip-testing harness |
| #4962 (f0f20d364) | run the multi-run RVCRUNM tests | **not applicable** | matter.js's own chip-testing harness |
| #4964 (1e79482d9) | run the multi-run SEAR tests | **not applicable** | matter.js's own chip-testing harness |
| #4965 (4557cf0e7) | run the multi-run TSUIC tests | **not applicable** | matter.js's own chip-testing harness |
| #4968 (7182e0280) | run the multi-run WNCV tests | **not applicable** | matter.js's own chip-testing harness |
| #4970 (6aff225db) | run the multi-run ACL tests | **not applicable** | matter.js's own chip-testing harness |
| #4971 (61d00497e) | run the multi-run BINFO tests | **not applicable** | matter.js's own chip-testing harness |
| #4972 (cb08d322a) | run the multi-run CADMIN tests | **not applicable** | matter.js's own chip-testing harness |
| #4973 (a8abe9c40) | run the multi-run DA tests | **not applicable** | matter.js's own chip-testing harness |
| #4975 (2da242f45) | run the multi-run OPCREDS tests | **not applicable** | matter.js's own chip-testing harness |
| #4978 (6afc55150) | run I 2.4 and LUNIT 3.1 again | **not applicable** | matter.js's own chip-testing harness |
| #4980 (146a0598e) | run the FLABEL tests | **not applicable** | matter.js's own chip-testing harness |
| #4981 (3da996b6c) | run the OCC 2.x tests | **not applicable** | matter.js's own chip-testing harness |
| #4982 (ace09612d) | run SOIL 2.1 | **not applicable** | matter.js's own chip-testing harness |
| #4983 (25a950f70) | run the DRYERCTRL tests | **not applicable** | matter.js's own chip-testing harness |
| #4984 (8317f5685) | report how many steps a CHIP python test skipped | **not applicable** | matter.js's own chip-testing harness |
| #4959 (224c2bf7f) | list each CHIP python test once in the test descriptor | **not applicable** | matter.js's own chip-testing harness |
| #4977 (3c96eddd7) | run the multi-run SC tests | **have it** (library rider: an ICD advertises SAI whatever its value, SII while it operates as SIT, `packages/protocol/src/advertisement/mdns/MdnsAdvertisement.ts:193-204` at b25ed960) | `mdns/service.go:504-520`, `:858-874` emit SII, SAI and SAT on every record, defaults included; that we emit them at their defaults where matter.js omits them is an existing difference, recorded as `BD-mDNS-SAT-AlwaysEmitted` |
| #4963 (0c5d1d007) | run the multi-run S tests | **caught up** (library rider: RemainingCapacity = min(own share, free entries), another fabric's entries updated, `packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:1055-1059` at b25ed960) → **implemented (P2b-A)**: the Go computation was already matter.js's; the comment mirrors `:1051-1059`; another fabric's change reports FabricSceneInfo (the digest holds the table length, `cluster/core:TestScenesRemainingCapacityIsBoundByTheTable`) | `BD-Matter-Scenes-RemainingCapacity` retired; parity `cluster/core:TestScenesManagementServerParityMatterJS` |
| #4967 (64c160bcb) | run the multi-run PS tests | **not applicable** | matter.js's own chip-testing harness |
| #4974 (edb586f07) | run the multi-run IDM tests | **not applicable** | matter.js's own chip-testing harness |
| #4986 (6b5b833cd) | run CHIP's lock, default-device, bridged and electrical-sensor runs | **not applicable** | matter.js's own chip-testing harness |
| #4989 (86f3e6e52) | select every run of a multi-run CHIP test that a glob names | **not applicable** | matter.js's own chip-testing harness |
| #4991 (e0b85854a) | report a CHIP python test that skips itself as skipped | **not applicable** | matter.js's own chip-testing harness |
| #4994 (2cc9c990b) | reopen command pipe after stop | **not applicable** | matter.js's own chip-testing harness |
| #4999 (c7ee35ff0) | select every run of a test a CHIP registration names without wildcards | **not applicable** | matter.js's own chip-testing harness |
| #4996 (9c6fb19ba) | drive the WebRTC requestor cases through adapter operations | **not applicable** (library rider: debug log lines in `WebRtcTransportRequestorServer.ts`) | the WEBRTCR-2.x cases now run through adapter operations; the `requestorServed` reasons (`internal/chiptool/families_table_test.go`) are re-read in the pin PR |
| #5029 (87b0f92b9) | --logfile parameter for the test apps | **not applicable** | matter.js's own chip-testing harness |
| #5004 (c3df89860) | decide once which chip-tool reply entries are the command's own | **not applicable** | matter.js's own chip-testing harness |

## 862ad75f → db021da0 (2026-11-06, milestone 10 after phase 39)

63 commits; 17 skipped (9 `[CHIP BUILD]`, 3 version bumps / the 0.18.0
release, 2 `chore`/`ci`, 3 Dockerfile-only harness images). Map:
`notes/reference/read-matterjs-delta-db021da0.md`. Model: no change under
`packages/model/src/standard/elements`, `packages/types/src/clusters` or
`packages/types/src/globals` — no cluster, attribute, command, event,
enum, revision or conformance moved; the snapshot's only change is
`matter.sourceCommit` (and the generators' source line in every generated
file). A behaviour lift, not the 1.7 pin (roadmap phase 40 waits for
chip's `data_model/1.7` in matter.js's snapshot). Owner decision
2026-11-06: the gaps are fixed before `v0.18.0`.

The register's markers at db021da0 (`make ahead-check` against
`origin/main`): **caught up** —
`BD-Matter-MessageCodec-PrivacyWithExtensions` (#4843: `decodeFixedHeader`
keeps the obfuscated region of a privacy packet and the MX flag,
`MessageCodec.ts:258-272`; the message extension is read length-checked
after the fixed header, `:328-340`; P on a unicast session refused "as in
the CHIP SDK", `:251-253`); our parser and the bridge already did the
same, the row closed, the entry kept as the record. A false alarm —
`BD-matterjs-server-NetworkTopology`: the absence marker is the body of
`class NetworkClient`, which #4808 edited (`WaitingForDeviceDiscovery` for
a peer with no address); no topology service appeared, the row stays and
its marker is re-read at the new pin. Every other marker holds. Also
caught up outside the register: `BD-Matter-CaseInitiatorPeerCATs` (#4884,
the initiator takes the responder's CATs from its NOC, `CaseClient.ts:282-290`).
Found by the re-read of review II's phase 4 (2026-11-14), also outside the
register: `BD-Matter-Controller-InboundWithoutSourceNodeIDDropped` (#4843,
the header row under Security below: the packet decoder refuses an
unsecured unicast message with neither node id, `MessageCodec.ts:234-238`,
which this module's exchange manager already dropped); the entry is retired
in `by_design.md`.

Citations that resolve at the pin — the bare `path:line` form without an
`at <sha>` — now resolve at db021da0: 18 pointed beyond the end of a
shrunken file (hard errors) and were re-read and updated, and the drift
count of `make cite-check` rose from 253 to 419 (`citecheckDriftRatchet`
raised in the lift's commit: a citation whose line moved without changing
what it says drifts, and a lift moves many at once; the drift is worked
off citation by citation as the files are touched, never by a sweep).
The 18 hard errors: seven files; re-read and
updated (`ServerBehaviorBacking.ts` quiet subscription `:251-259` /
`#configureEventSuppression` `:223-262`, `PeerExchangeProvider.ts:19-159`
/ `:150-158` at db021da0, `validateNocTagList` now `Noc.ts:116`, the CADMIN flows in
`tc-cadmin-support.ts`, `tc-dd-flow-support.ts:72-189`, `MdnsTest.ts`
`expectMessage` `:1001-1024`, `Rcac.ts:135`).

The matter.js test register (`script/testparity/register.json`) moved with
the snapshot: 56 entries re-checked against their new blobs (395 new `it`
cases listed as missing, 30 removed cases pruned, 11 ported files now
partial), 3 files gone (the WAL cleaner and snapshot tests, the peer
establishment progress test), 24 new files entered — 6 excluded as JS
runtime, 18 planned on the lift's waves P1–P4. Ratchets: planned 2 → 20,
ported 158 → 146, missing 583 → 977, left 593 → 592. The missing cases are
the waves' port lists.

### Security

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4884 | the CASE initiator takes the peer's CATs from its NOC in Sigma2 (`CaseClient.ts:282-290`); resumption records versioned | **caught up** at the sigma layer; **gap → lift wave P1**, closed 2026-11-06, at the client stack | `secure/sigma/initiator.go` `processSigma2` agreed; `client/handshake/case.go` gave an initiated session the configured or recorded CATs, which `client/interaction/server.go` takes as the peer's subject CATs — wave P1 makes them the peer's NOC CATs and removes `CASEOptions.CaseAuthenticatedTags` and `ResumptionRecord.PeerCATs` (BD-Matter-CaseInitiatorPeerCATs); the record version is a JS storage detail, the module's records live in memory |
| #4843 | group decode checks GroupKeyMap for the destination group (`GroupSession.ts:37-60`) | **have it** | `groups/decode.go:27-111` (a message belongs to the key set GroupKeyMap maps the group to) |
| #4843 | the fixed header refuses a group session message with a destination node id (MCSP, `MessageCodec.ts:303`, as chip), and an unsecured unicast message with both or neither of source and destination node id (`:305`) | **gap → lift wave P1**, closed 2026-11-06 (branch `pin/p1-security`) | `transport/message/message.go` refuses MCSP (`ErrGroupSessionHeader`) and an unsecured unicast message with both or neither node id (`ErrUnsecuredNodeIDs`); `MarshalPacket` the same; the bridge's unsecured replies and acknowledgements name one node id (`bridge/ackpump.go`, `bridge/reply.go`) |
| #4843 | the privacy flag on a unicast message refused (`:251-253`) | **have it** | `bridge/privacy.go` `maybeUnmaskPrivacy` |
| #4843 | AES-CCM input limits shared by the two crypto implementations | **not applicable** | Go's stdlib AEAD |
| #4865 | DAC/PAI certificate profile enforced in device attestation: version 2, basic constraints and key usage present and critical, no duplicate extension; PAI cA TRUE + pathLen 0 + keyUsage, DAC cA FALSE (`AttestationCertificates.ts:85-200`, `DacFormatInvalid`/`PaiFormatInvalid`) | **gap → lift wave P2**, closed 2026-11-06 | `controller/attestation/profile.go` (the DER encoding beside `crypto/x509`, `assertDacProfile`/`assertPaiProfile`), `cert.go` `parseAttCert` per kind, `validate.go` `parsePeerCertificate` (PAI first, `DacFormatInvalid`/`PaiFormatInvalid`); chip's commissioner DUT vectors in `testdata/chip-commissioner-dut`; `BD-Matter-Attestation-ProfileOnCryptoX509` |
| #4871 | Certification Declaration validated as chip does: CMS envelope, `format_version` 1, certification type, 20-byte signer SKID (`DeviceAttestationValidator.ts:456-515`); an unknown CD signer is `CdSignerUnknown`, an error, with a built-in trusted-signer set (`CdSigners.ts`) | **gap → lift wave P2**, closed 2026-11-06 | `controller/attestation/cd.go` `parseDeclaration` (`CertificationDeclarationFormatInvalid`), `validate.go` (format_version, reserved type, `OfficialCdSignedByTestSigner`, `CdSignerUnknown`, the test signer as `TrustedAsTestCertificate`), `cdsigners.go` (`KnownCDSigner`, `VerifyCDSignerCertificate`); `controller/dcl` `CertificateStore.CDSigner`/`AddCertificate` and `snapshot` trust CSA-issued signers only; `BD-Matter-Attestation-CDSignerTrust` |
| #4880 | operational certificates: a repeated single-valued DN field refused (`DistinguishedName.ts:242-246`, `:261-283`); matter-vvs-id (tag 23) decoded in NOC/ICAC/RCAC and refused there; a NOC must list serverAuth (`Noc.ts:148-154`); a VVSC with is-ca false accepted and its extensions checked (`Vvsc.ts`, `VendorIdVerification.ts`) | **gap → lift wave P1**, closed 2026-11-06 for the certificate rules; the VVSC chain check of the verifier (`VendorIdVerification.ts:231`) is #433's, closed by lift wave P4 2026-11-06 (`secure/mattercert/vvsc.go` `DecodeVVSC`, `ValidateVVSC`, `VerifyVVSCIssuedBy`; `client/jcm.Verify`'s statement path, `BD-Matter-VIDStatementVerification`) | `secure/mattercert/decode.go` and `der.go` refuse a repeated Matter-specific DN attribute and decode matter-vvs-id (`DistinguishedName.MatterVVSID`); `verify.go` `checkDNLimits` (five RDNs), serverAuth required of a NOC, matter-vvs-id refused in a NOC, ICAC or RCAC subject; the device keeps a set VVSC opaque, as `Fabric.ts:231` does |
| #4880 | a CAT with version 0 refused; at most three CATs; a CAT id twice refused | **have it** | `secure/mattercert/verify.go:457-470` |
| #4899 | message counter window edges (tests and comments; the "new" range ends at 2^32-1) | **have it** | `transport/mrp/window.go:12-62`; the edge tests are a parity port candidate (open, low) |
| #4879 | resumption records and server subscriptions stored per peer | **not applicable** | JS storage layout; `store` keys them by fabric and node already |

### Device side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4878 | AdministratorCommissioning: a discriminator above 0xFFF is INVALID_COMMAND in the handler, not CONSTRAINT_ERROR (`AdministratorCommissioningServer.ts:42-46, :91-97`); an open on a session without fabric answers PAKEParameterError; the check order Busy → window → parameters | **gap → lift wave P1**, closed 2026-11-06 | `cluster/wire/admincommissioning.go` (`assertNoCommissioningInProgress`, the fabric check first as PAKEParameterError, the discriminator last as INVALID_COMMAND); `bridge/fields_reader_admincomm.go` decodes any discriminator |
| #4881 | GeneralDiagnostics: `systemTime()` shared by UpTime and TimeSnapshot.SystemTimeMs, suspend-aware high-water mark (`GeneralDiagnosticsServer.ts:61-86`); TestEventTriggersEnabled derived from the enable key (true iff not all zero, `:144-156`); one gate for TestEventTrigger and PayloadTestRequest | **gap → lift wave P3**, closed 2026-11-06 | `cluster/core/general_diagnostics.go`: TimeSnapshot's SystemTimeMs reads `upTime` (the high-water System Time); `EnableTestEventTriggers` derives TestEventTriggersEnabled from the key (an all-zero key disables, a nil handler answers InvalidCommand); `assertEnableKey` gates both test commands; tests `cluster/core/general_diagnostics_server_parity_matterjs_test.go` |
| #4844 | mDNS goodbyes only for records no remaining advertisement publishes; failed goodbyes retried with backoff (`InterfaceResponder.ts:46-67, :263-280`) | **gap → lift wave P3**, closed 2026-11-06 | `mdns/publication.go`: a per-interface peer cache noted by every send as far as it reached; one publication per interface carries the goodbyes of the records that left (cache-flush bit per RFC 6762 §10.2) and the announcement, a replacing service included; goodbye retry 1 s doubling to 1 min; `mdns/socket.go` sends report their reach; tests `mdns/publication_parity_matterjs_test.go`. Left: the one-second rule on every multicast with deferral, and the same-turn announcement batching (register `missing`) |
| #4842 | the responder follows interface address changes (polled every 60 s while observed): rebuilds records, goodbyes removed addresses, announces twice 1 s apart; GeneralDiagnostics re-reads NetworkInterfaces on change (`MdnsServer.ts:288-348`, `GeneralDiagnosticsServer.ts:112-115, :389-401`) | **gap → lift wave P3**, closed 2026-11-06 | `internal/netwatch` (the Network's comparison), `mdns/follow.go` (publication on the changed interfaces, repeat after 1 s; host name stays the Service's, `BD-Matter-MdnsFollowsAdvertisedAddresses`), `cluster/core/general_diagnostics.go` `OnAttributeChange` (NetworkInterfaces reported while observed); `node.Network.InterfaceCheck` sets or disables the interval |
| #4841 | Thermostat Presets validated against the presets the transaction replaces (built-in and active-removal rules) | **have it** (wave P3, 2026-11-06) | `cluster/thermo/presets.go` `normalizePresetCommit` checks the commit against the stored presets it replaces (`atomic.go` passes `s.presets`): a removed built-in preset is CONSTRAINT_ERROR, the removed active preset INVALID_IN_STATE, a null BuiltIn resolved from the stored preset — `ThermostatServer.ts:1300-1311, :1516-1580` at db021da0 |
| #4900 | UserLabel and FixedLabel server tests (test-only; any server fix inside them is read in wave P3) | **ported** (wave P3, 2026-11-06) | `node/label_parity_matterjs_test.go` (`TestUserLabelServerParity`, `TestFixedLabelServerParity`); one fix: FixedLabel's LabelList joins `schema/writable.go`'s read-only table, so a controller's list ADD is UNSUPPORTED_WRITE; the lone ADD answered BUSY by matter.js (`InteractionServer.ts:511-531`) is an IM write-path gap, findings register T3-P3-1 |
| #4795 | a debug line when a node does not advertise because commissioning is disabled | **not applicable** | log only |
| #4825, #4811, #4841 (rest), #4902 | managed state references, decorator derivation, typings | **not applicable** | JS runtime |

### Controller side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4793 | the TCP send path is capped at min(channel, the peer's announced MAX_TCP_MESSAGE_SIZE); an oversized message is `MessageTooLargeError` (`MessageChannel.ts:87-112`, `Session.ts:230-241`) | **gap → lift wave P4**, closed 2026-11-06 | `client/exchange` `Session.PeerMaxTCPMessageSize` (the announced size, 64000 without one, never below 1232) caps `Session.MaxPayloadSize` under the channel's limit, and a larger message is `ErrMessageTooLarge` before it reaches the channel (`checkSize` in `transmitLocked`); `transport/tcp`'s limit excludes the length prefix and has no receive-buffer cap (`TcpChannel.ts:37-41`, `:204`); `bdx` offers at most 0xFFFF (`bdx-session-initiator.ts:155`); `client/exchange:TestMessageChannelTCPLimitParity`, `transport/tcp:TestTcpChannelOversizedMessageHandling` |
| #4794 | a Sigma1 the peer never acknowledges is refreshed on a new exchange with a fresh ephemeral key after the MRP transmissions, per address, with the backoff continued (`InitialContactSchedule.ts:1-246`) | **gap → lift wave P4**, closed 2026-11-06 | `client/peer/initial_contact.go` (`initialContactSchedule`, one per address; the attempt's Sigma1 options, the unresponsive signal and the kick blocker), `connection.go` `attemptOnce` retrying at once after an unacknowledged Sigma1; `exchange.SendOptions` `RetransmissionTimeLimit` and `BackoffOffset`, `handshake.CASEOptions` `InitialRetransmissionTimeLimit` and `InitialBackoffOffset`; `client/peer:TestInitialContactScheduleParity`, `client/peer:TestConnectionRefreshesUnacknowledgedSigma1Parity` |
| #4832 | delta event timestamps resolved per ReportData message against the previous event of the same clock (`InputChunk.ts:359-412`), `ReadResult.EventValue.resolvedTimestamp` | **gap → lift wave P4**, closed 2026-11-06 | `client/interaction/report.go` `eventTimestampChain` resolves per ReportData message, `EventData.ResolvedTimestamp` carries it to `Report.Events` and `controller/client` `Hooks.Event`; `client/interaction:TestInputChunkResolvedTimestampsParity`, `controller/client:TestClientEventNotificationResolvedTimestampsParity` |
| #4808 | a peer with no address at all reports WaitingForDeviceDiscovery (`NetworkClient.ts:541-546`) | **gap (small) → lift wave P4**, closed 2026-11-06 | `controller/client/network.go` `hasAnyAddress` (a session, the operational address or an address of the peer's operational service) feeds `recompute`, and `observePeer` recomputes on every change of that service (`NetworkClient.ts:88`, `:390-392`); `controller/client:TestConnectionStateWithoutAddressParity` |
| #4815 | the fail-safe re-arm between commissioning steps is skipped on a closed session (`ControllerCommissioningFlow.ts:835-849`) | **gap → lift wave P4**, closed 2026-11-06 (the periodic re-arm already skipped; the between-steps one did not) | `controller/commissioner/flow.go` `reArmFailsafeBetweenSteps`, called before a step with a re-arm and after a step that left less than half the failsafe (`ControllerCommissioningFlow.ts:385-393`, `:409-423`); `controller/commissioner:TestCommissioningClosedSessionReArmParity` |
| #4812 | legacy node enumeration skips unreadable peers | **not applicable** | the JS package's legacy API |
| #4809 | shell: legacy peer migration | **not applicable** | JS shell |

### Harness

#4792, #4814, #4819, #4820, #4823, #4828, #4845, #4850, #4851, #4852,
#4853, #4855, #4857, #4866, #4870, #4875, #4876, #4877: cert cases and
chip-testing adapters of matter.js's own harness — **not applicable**;
the cases they add (TC-CADMIN-1.1/1.2/1.18, TC-DA-1.3/1.4/1.8, TC-DD-1.9/
2.2/3.5, TC-CC-x.4/x.5, TC-TSTAT-3.2, TC-DRLK-3.2, TC-OO-3.2, TC-I-3.2,
TC-MOD-2.2, TC-LOWPOWER-2.2, TC-WNCV-6.1/7.1, TC-DGETH/DGSW/DGTHREAD/DGWIFI
3.x) are the Go-TH leg's next candidates (`internal/chiptool/gocert_*`),
tracked in `docs/certifiability.md` and, since review II's phase 4, held
there by `TestCertifiabilityDocument` against
`internal/chiptool/testdata/matterjs-cert-cases.json`: each case at the pin
is run or classified with what a port needs.

## Milestone 6 (2026-10-28, wave 27-C) — what the next lift re-reads

No lift; the pin stays `862ad75f`. Milestone 6 added 18 rows to the
*Ahead of matter.js* register (12 before wave 26-B, 30 after it; 39 at
the review of 2026-11, `make ahead-check` holding every marker at the pin
and at HEAD). The next lift
re-reads each of their markers first, because each names a matter.js
server that was the generated stub at the pin:

- phase 26, camera (waves 26-A1 … 26-A3): `BD-chip-Chime-RulesInServer`,
  `BD-chip-CameraAvSettings-RulesInServer`,
  `BD-chip-CameraAvStreamManagement-RulesInServer`,
  `BD-chip-ZoneManagement-RulesInServer`,
  `BD-chip-WebRtcTransportProvider-RulesInServer`,
  `BD-chip-PushAvStreamTransport-RulesInServer`,
  `BD-chip-TlsCertificateManagement-RulesInServer`,
  `BD-chip-TlsClientManagement-RulesInServer`;
- phase 26, network infrastructure (wave 26-B):
  `BD-chip-WiFiNetworkManagement-RulesInServer`,
  `BD-chip-ThreadNetworkDirectory-RulesInServer`;
- phase 26a, the Thread Border Router: `BD-Thread-TBRM-ChipLogic`,
  `BD-Thread-TND-FromBorderRouter`, `BD-Thread-NetComm-ThreadVariant`;
- phase 26, sensing (wave 26-C): `BD-chip-AmbientContextSensing-RulesInServer`;
- phase 26, Joint Fabric (wave 26-D):
  `BD-chip-JointFabricDatastore-RulesInServer`,
  `BD-chip-JointFabricAdministrator-RulesInServer`, `BD-chip-JCM-Commissioner`;
- phase 27 (wave 27-B): `BD-chip-TimeSynchronization-Features`.

The Thermostat events (TEVT, wave 27-B) have their own register row since
2026-10-29, `BD-chip-Thermostat-Events` (its marker: `ThermostatServer.ts:525`
is the server's only `events.` emit), so a lift re-reads them with the
other markers.
WebRtcTransportRequestor mirrors matter.js and is no register row.

## 0ceb202d → 862ad75f (2026-10-18, milestone 2 phase 14)

57 commits; 16 skipped (7 `[CHIP BUILD]`, 7 version bumps, `ci:` #4787,
`docs:` #4782). Map: `notes/reference/read-matterjs-delta-862ad75f.md`.
Model: one element change, `bridged-node.element.ts:17` — BridgedNode
`composition: "full-family"` (#4735); no cluster, attribute, command, event or
revision moved. Snapshot regenerated in commit `9a6c2fb` (brief BD's branch).

### Device side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4735 | BridgedNode PartsList lists every descendant (`DescriptorServer.ts:227-270`) | **gap → brief BD item 1** | `endpoint/assembler.go:230-238` lists direct parts |
| #4766 | keep-alive DataReport with `suppressResponse: false` so INVALID_SUBSCRIPTION is read on every transport (`ServerSubscription.ts:913-914`) | **gap → register** | `bridge/subscribe.go:447-453` sets `SuppressResponse = true` (the rule this commit reverses) |
| #4742 | a subscription that ends while the node runs is dropped from the store; only `closeForShutdown` keeps it (`Subscription.ts:21-40`, `ServerSubscription.ts:766-812`, SubscriptionsServerTest) | **gap → register** | `im/subscription/manager.go:128-149` keeps the former rule: closed on session close = re-established after restart |
| #4768 | server: another exchange's write of an attribute a chunked write holds is answered BUSY (`InteractionServer.ts:507,526`) | **closed (phase 31)** | `im.WriteHolds` (`im/write.go`), one table per bridge (`bridge/exchange_routing.go` `writeHolds`): BUSY for a concrete write, a wildcard or group write skips the attribute; `im:TestChunkedWriteHoldParityMatterJS` |
| #4775 | KeypadLockout stays as configured or stored; NoLockout only when undefined (`ThermostatUserInterfaceConfigurationServer.ts:30-32`) | **gap → register** | `cluster/thermo/thermostatuserinterfaceconfiguration.go:31-32` resets on every start |
| #4736 | OTA provider: `NewVersion 0` under a known token applies the token's version; another version than the token's is refused Cancelled (`OtaSoftwareUpdateProviderServer.ts:419-458`) | **gap → register** | `cluster/core/ota_provider.go:558-563` checks consent on the raw `NewVersion` only |
| #4745 | a utility component the composing endpoint lists itself counts that endpoint as a candidate (`DeviceTypeConformance.ts:596-614`, Core § 9.2.1) | **gap → register** | `endpoint/devicetype_conformance.go:842-850` iterates the composition scope only |
| #4746 | a condition true only by a `Root` assertion never refuses; the violation is marked `assertedAtRoot` and logged (`DeviceTypeConformance.ts:65-76`, `ConditionAssertions.ts:63-68`) | **closed (phase 31)** | `endpoint/devicetype_conformance.go` `check` re-checks the node endpoint without its Root-asserted conditions and flags `DeviceTypeViolation.AssertedAtRoot`; `DeviceTypeValidator` never refuses a flagged violation and reports it in `DeviceTypeVerdict.FreshAssertedAtRoot`; `endpoint:TestDeviceTypeConformanceServiceParityMatterJS` |
| #4779 | a replaced Fabric shares groups, ACL and ICD state with its replacement | **have it** (by construction) | `handleUpdateNOC` updates the NOC of the session's fabric index in place; ACL, groups and ICD state are keyed by fabric index and untouched |
| #4778 | only a change of the fabric the armed fail-safe owns is deferred; UpdateFabricLabel / SetVIDVerificationStatement under a fail-safe without AddNOC/UpdateNOC are final | **have it** — settled by the OperationalCredentialsServerTest port (ported): `node:TestOperationalCredentialsFinalFabricChange` treats UpdateFabricLabel and SetVIDVerificationStatement under a fail-safe without AddNOC/UpdateNOC as final fabric changes, reported to `Observers.OnFabricUpdated` (review 2026-11 W6-reg-11) |
| #4777 | client answers the priming report of a cancelled subscribe with INVALID_SUBSCRIPTION; the server drops it | **have it** — the priming report's chunk loop waits for the peer's status through the shared chunk sender and aborts the Subscribe on an error status (`sendChunk`, `bridge/chunk_send.go`, → `awaitChunkStatusResponseWithin`, `bridge/receive_dispatch.go`, as matter.js's `waitForSuccess`, `packages/protocol/src/interaction/InteractionMessenger.ts:844`), and the subscription admitted for the priming is closed, as a failed `sendInitialReport` closes it and rethrows (`packages/node/src/node/server/InteractionServer.ts:828-834` at `db021da0`). Test: `bridge:TestHandleSubscribeRequest_PrimingReportRefused_EstablishesNothing` (review II, W6-reg-11; until 2026-11-13 "have it, untested") |
| #4765 | an out-of-range vendor ID in a manual code is an invalid payload | **have it** — `secure/setup/decode.go:96-107` validates through `assertValidPayloadIdentity`, which returns the invalid-payload error |
| #4759 | AtomicRequest: request-wide checks before per-attribute statuses | **not applicable** — no atomic-write handler here (thermostat presets, milestone 3; chipdm acknowledges the command) |
| #4737 | WebRTC requestor sessions keyed per peer | **not applicable** — no WebRTC server (milestone 6 stubs) |
| #4734 | ML-DSA attestation certificates skipped at debug | **not applicable** — ML-DSA is not admitted here (`controller/dcl/revocation.go:389-391`); no fetch or store path for such a PAA |
| #4758 | IPv4 multicast membership on the configured interface | **not applicable** — IPv6 only (`transport/udp/multicast.go`) |
| #4731 #4732 #4748 #4749 #4762 #4771 | reference lifetimes, migration hook, WebSocket, console format, transaction lock, cache lifetime | **not applicable** — JS runtime |
| #4738 #4772 #4774 #4776 #4786 #4717 #4788 (harness part) | matter.js test harness | **not applicable** |

### Controller side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4741 | DNS-SD TXT keys match regardless of case, first wins (`DnssdParameters.ts:15-49`, RFC 6763 § 6.4) | **gap → register** | `controller/discovery/name.go:154-162` `Parameters` is a case-sensitive map |
| #4756 | known answers carry their remaining TTL and are left out past half TTL (`DnssdSolicitor.ts:280-291`, RFC 6762 § 7.1) | **gap → register** | `controller/discovery/solicitor.go:368-375` copies records unchanged |
| #4770 | a client node is seeded once BasicInformation and every endpoint of the root's PartsList are in; a root-only peer qualifies (`ClientNodeLifecycle.ts:52-56`) | **gap → register** | `controller/client/config.go:116-118`, `node.go` `evaluateSeeded` keep "more than the root endpoint" |
| #4747 | discovery error kinds (`DeviceNotDiscoveredError`, `DiscoveryIncompleteError`, `NoDiscoveryTransportError`) and an abort that stops every attempt | **gap → register** (API) | `controller/discovery`, `controller/commissioner`: one error, no abort option |
| #4750 | a status the peer sent is told from one produced locally (`ResultStatus.received`) | **gap → register** (API, low) | `controller/interaction/interaction.go:56` `StatusError` has no origin |
| #4752 | a closed sustained subscription never reports itself active | **have it** — settled by the SustainedSubscriptionTest port: `client/interaction:TestSustainedSubscriptionNonICD` "never reports active when closed while the subscribe is in flight and then succeeds" (review 2026-11 W6-reg-11) |
| #4751 | one owner for a client node's event number and data versions | **have it** for the event number — settled by the ClientEventNotificationTest port (partial): `controller/client:TestClientEventNotificationEventNumbersParity` holds one stored event number, never lowered, asked above by a replacement subscription and after a restart; the data versions are the client image's own (the version filters it injects); the per-subscription event number stays `missing` there (review 2026-11 W6-reg-11) |
| #4760 | every ClientNode write reaches the device, the cache follows the device | **have it** — no local cache write exists; the image changes when the peer reports (`controller/client/interact.go:100-110`) |
| #4767 | invoke batching per request options | **not ported** (documented, `controller/interaction/interaction.go:12`); unchanged |
| #4769 | ICD Check-In key refresh kept in step | **not applicable** until milestone 5 (no ICD client) |
| #4733 | controller-side OTA: an update applied without restart fails | **not applicable** until milestone 5 (no controller OTA manager) |
| #4788 | `caseConnectionTimeout` commissioning budget | **have it** — settled by the CaseConnectionTimeoutTest port (ported): `controller/client:TestCaseConnectionTimeoutParity` fails a commissioning whose CASE connection outlasts the budget and refuses a non-finite budget (review 2026-11 W6-reg-11) |
| #4743 | `PeerDescriptor.dataModelRevision` removed | **not applicable** (type only) |
