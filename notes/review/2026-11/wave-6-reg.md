# W6 — register currency (`reg`)

- **Scope:** `notes/parity/matter_behaviour_findings.md` (every FIXED, "Still open", open and deferred entry), `notes/parity/by_design.md` (every `### BD-…` entry, the audit tables at :652-712, the *Ahead of matter.js* register :34-118, the "Removed" section), `notes/parity/pin-lifts.md`, `notes/parity/controller_profiles.md`, `notes/parity/matterjs_tests.md`, and 60 of the 906 non-test `// Mirrors matter.js …` comments (bridge 14, im 12, cluster/core 12, secure 10, other 12; list in the reviewer's scratchpad `mirrors_sample.txt`).
- **HEAD:** `769ea3c2`. **Pins:** matter.js `862ad75f` (`git -C ../matter.js rev-parse HEAD` = `862ad75f4d3c…`); connectedhomeip `6170af84` (read with `git show 6170af84:<path>`; the working tree is at `57f29895`).
- **Method:** the findings register, pin-lifts, controller profiles, the by_design audit tables and every high/medium candidate were read by the reviewer on both sides. The 274 `### BD-…` entries, the matterjs_tests rows and the 60 Mirrors positions were screened first by four plus two Sonnet reads (maps, not citations). Every finding below was re-read by the reviewer except where marked *plausible*. `make ahead-check`: "32 rows, 32 with a marker … every marker holds at the pin 862ad75f". `go test ./script/profilegen/`: ok.
- **Tally:** entries checked 926 (findings register 96 + 4 batch notes, by_design 274 `###` entries + 32 Ahead rows + 30 table rows + Removed, pin-lifts 31 rows + the milestone-6 section, controller_profiles 1, matterjs_tests 396, Mirrors 60). Stale citations about 85 (by_design 58, Mirrors 18, findings register 6, pin-lifts paths 3). False claims 31 (1 hides a real security defect, 1 a real parity defect; 4 open items already fixed; 22 by_design divergences that no longer exist on the go-fabric side; 2 Mirrors behaviours matter.js does not have; 1 inverted divergence). Caught-up divergences 1 (not a divergence at the pin either).
- **Findings:** high 1, medium 7, low 7, note 2.

## W6-reg-01 — high — a FIXED entry hides the missing PASE close on CommissioningComplete and on fail-safe expiry

- **Register:** `notes/parity/matter_behaviour_findings.md:306`.
- **Claim:** The entry says FIXED: "The daemon's CommissioningComplete hook closes them after a 100 ms grace …, the fail-safe-expiry hook closes them immediately". The only closer, `operational.Manager.ClosePASESessions` (`secure/operational/manager.go:1027`), has no non-test caller in the module (grep). The node facade's CommissioningComplete hook (`node/root.go:232-242` → `node/node.go:664-673`) and fail-safe-expiry hook (`node/root.go:214-221`) close no session. The hook lived in the host daemon. It was never in this module: `git grep ClosePASESessions b4ea1f96^` finds only the definition and the register text. matter.js closes the session in both places (`FailsafeContext.ts:153` `closePaseSession` in `completeCommission`, `:279` on expiry). This is the defect W2-sec-02 confirmed. This entry is why no wave picked it up.
- **Correction:** reopen the entry as open (wave H2, with W2-sec-02), and drop the daemon sentence.
- **Confidence:** confirmed.

## W6-reg-02 — medium — H1's FIXED claim that UpdateNOC drops the fabric's resumption records is a host-daemon claim; the module does not drop them

- **Register:** `notes/parity/matter_behaviour_findings.md:243` (H1: "the daemon's fabric-updated hook now drops the fabric's resumption records (`FailsafeContext.ts:167-170` replaceFabric)").
- **Claim:** In the module the fabric-updated hook is `node.observedFabricUpdated` → `fabricUpdated` (`node/node.go:589-602`). It reloads the CASE identity, re-announces and calls `CloseFabricSessionsAfterUpdate`. It touches no resumption record. `store.RemoveResumptionsByFabric` (`store/resumption.go:148`) has no caller outside the store's own tests. UpdateNOC updates the fabric row in place (`store/fabric.go:225` `UpdateFabricNodeID`), so the `ON DELETE CASCADE` that clears records on RemoveFabric (`store/memstore/memstore.go:261-283`) never fires. matter.js at the pin: `FailsafeContext.replaceFabric` (`:167-170`) runs `this.#sessions.deleteResumptionRecordsForFabric(fabric)` (`SessionManager.ts:491`). Result: after UpdateNOC a peer can still resume with the record from before the update.
- **Correction:** reopen the resumption half of H1 as open. The fix is `RemoveResumptionsByFabric` in `node.fabricUpdated`, and the entry should be rewritten without "daemon".
- **Confidence:** confirmed.

## W6-reg-03 — medium — seven by_design entries on the security and commissioning paths record gaps the module has since closed

- **Register:**
  - `by_design.md:1318` (BD-Matter-P2-D24), `:1374` (BD-Matter-P2-D23), `:1430` (BD-Matter-v13-D-AccessControl-Extension-WriteHandler);
  - `:1420` (BD-Matter-P2-D13-18);
  - `:1392` (BD-chip-P1-D-L8-NEW-2), `:665` (L3-PFAD-6);
  - `:1402` (BD-chip-P2-D-L9-NEW-1).
- **Claim:**
  - **AccessControl Extension.** The three Extension entries say Extension is read-only, always empty and `ExtensionChanged` never fires. `cluster/core/access_control.go:888-892` implements the Extension write ("Mirrors matter.js AccessControlServer.ts"), and `:951-979` emits the event. `TestAccessControl_Extension_WriteAndReadBack` (`access_control_extension_test.go:104`) pins it.
  - **SetVIDVerificationStatement.** D13-18 says it "returns `InvalidCommand` unconditionally". It is implemented at `cluster/core/vid_verification.go:115`.
  - **PASE auto-arm.** L8-NEW-2 says PASE does not re-arm the fail-safe to 60 s. L3-PFAD-6 calls the auto-arm an optional hook. `node/security.go:617` calls `AutoArmOnPaseEstablished` on every PASE session. That is matter.js's `GeneralCommissioningServer.ts:55-65` ("any new PASE session needs to arm the failsafe for 60s").
  - **AddNOC rollback.** L9-NEW-1 defers a canonical rollback helper. `revertAddNOC` exists (`operational_credentials.go:2412`).
- **Correction:** retire the seven entries, or rewrite them as "closed, now as matter.js / chip".
- **Confidence:** confirmed.

## W6-reg-04 — medium — fifteen further by_design entries describe a go-fabric divergence that no longer exists

- **Register and evidence** (each read on the go-fabric side):
  - `:670` L6-PFAD-3, "burst-sendet ohne intermediate Status": `bridge/subscribe.go:725-797` waits for the StatusResponse to each chunk but the last. This is now as matter.js.
  - `:1412` BD-chip-P2-D-L10-NEW-1, ICD MaxInterval "must be added to `clampMaxInterval`": implemented as `SetICDIdleModeDuration` (`im/subscription/manager.go:139-148`), wired at `node/icd.go:236`. `clampMaxInterval` does not exist.
  - `:1440` BD-Matter-v13-D-mDNS-FabricChange-ReAnnounce, no immediate announce on a fabric change: `node/node.go:504` and `:599` call `Bridge.AnnounceFabric` (`bridge/bridge.go:2071`), and `:517` calls `WithdrawFabric`.
  - `:1382` BD-chip-P2-D-L4-NEW-1, "every list write is a whole-list replace": `endpoint/dispatcher.go:335-350` handles `ListAppend`. Only the "no ListWriteBegin/End notifier" part still holds.
  - `:1326` BD-Matter-P1-D9, WindowCovering Mode "not persisted": the module's server stores and reads it back (`cluster/cover/windowcovering_server.go:415-416, 495-545`). The cited `validateWindowCoveringMode` is host code.
  - `:408` BD-Matter-ButtonPressCycleFromDiscreteFrames, guarded by `TestParityMatterJS_GenericSwitchPressCycleSequences`: no such test exists (grep).
  - `:2359` BD-Matter-Binding-StoreOnly, "no client role … nothing acts on the list": `binding/` exists and is wired at `node/client.go:187-202`. The findings register closed the gap (`matter_behaviour_findings.md:792`).
  - `:2992` BD-Matter-WiFiHost-NetworkCommissioningEthernet, "reverts when a Wi-Fi commissioning port exists": the port exists (`node/config.go:274-280` `WiFiCommissioning`). Only the default stays Ethernet.
  - `:3431` BD-Matter-ClientUndefinedClusters, 16 clusters without a definition: `controller/client/known.go` links 135 `cluster/spec/` packages, which is every cluster of `parity/schema.json`.
  - `:3747` BD-Matter-Controller-SingleCommandBuilders, "one command per invoke": `client/interaction/invoke_batch.go:50` `InvokeBatch`.
  - `:4604` BD-Matter-DefaultOTAProviders-CompareWithoutFabricIndex: `controller/softwareupdate/announcements.go:178-195` builds the record with `FabricIndex` and compares the whole struct, as matter.js does.
  - `:4012` BD-Matter-SelfHeldDefault-SelectionNotServed lists "DoorLock without features" as not served: `TestDoorLockDefaultTakesTheDeclaration` (`device/defaults_coverage_test.go:241-255`) asserts the module's `*lock.DoorLockServer` for `Features: 0`.
  - `:3538` BD-Controller-ThreadDatasetNameOnly, "decodes the name TLV only": `controller/commissioner/network.go:84-92` decodes the whole dataset through `thread.DecodeDataset`.
  - `:2624` BD-Matter-DoorLock-HostFailureStatus calls the typed host error an open item in `docs/open-items.md`: that file does not exist, and `LockOperationFailedError` is implemented (`cluster/lock/doorlock_server.go:181-196`).
  - `:2563` BD-Matter-Thermostat-CompanionsAfterSink, "SetpointRaiseLower has no reconcile until the deadband finding is fixed": the finding is closed (`matter_behaviour_findings.md:781`), and `thermostat_server.go:1389` keeps the deadband.
- **Correction:** retire or rewrite each entry against the current code in one notes commit. Keep only the residue (no list notifier, Ethernet default).
- **Confidence:** confirmed (the Thermostat and DoorLock items from the delegated read, spot-checked; the rest re-read).

## W6-reg-05 — medium — BD-Matter-TimeSync-NotMounted is inverted: the module mounts TimeSynchronization by default, matter.js does not

- **Register:** `notes/parity/by_design.md:709`.
- **Claim:** The row records "not mounted by default", because Apple's mapper may reject unexpected root clusters. It cites the host's `cmd/openccu-loom/daemon_matter.go`. In the module, `node.Config.DisableTimeSynchronization` reads "the zero value mounts it, as the facade always did" (`node/config.go:150-158`). matter.js at the pin lists TimeSynchronization under `optional` (`packages/node/src/endpoints/root.ts:300`). Its `RootEndpointDefinition` behaviors are the mandatory set only (`:320-330`), and `ServerNode.RootEndpoint` adds Groupcast but no TimeSynchronization (`ServerNode.ts:282-301`). The real divergence, mounted by default, is not recorded. The row's matter.js cite `root.ts:215` has drifted to `:300`.
- **Correction:** replace the row with the module's actual default and its reason, or change the default to off. This is an owner decision.
- **Confidence:** confirmed.

## W6-reg-06 — medium — BD-Matter-ExchangeOnClosing-OnDestroy records a divergence matter.js does not have at the pin

- **Register:** `notes/parity/by_design.md:4593`.
- **Claim:** The entry says matter.js releases the network-profile slot only on `closing`, which the destroy path does not emit (citing `Peer.ts:168-170`). At `862ad75f`, `PeerExchangeProvider.ts:137-142` reads "An exchange that is destroyed rather than closed emits only "closed", so both paths must release", with `exchange.closing.once(releaseSlot); exchange.closed.once(releaseSlot);`. `Peer.ts:168-170` is the session-removal hook. Go's `notifyClosing` on close and on destroy (`client/exchange/exchange.go:954-960`) is therefore matter.js's behaviour.
- **Correction:** retire the entry ("same as matter.js, `PeerExchangeProvider.ts:137-142`"), and record it in pin-lifts only if the next lift shows the change came after the pin.
- **Confidence:** confirmed.

## W6-reg-07 — medium — `bridge/reply.go:267` mirrors a unicast privacy transform matter.js does not have; the path is dead code

- **Register:** `bridge/reply.go:261-269` ("Mirrors matter.js packages/protocol/src/codec/MessageCodec.ts encodeMessage, which applies the privacy transform whenever the session negotiated it").
- **Claim:** There is no `encodeMessage` in `packages/protocol/src` at the pin (grep). matter.js sends unicast with `hasPrivacyEnhancements: false, // Privacy is only defined for group messages` (`MessageExchange.ts:718`). On receipt it drops a unicast message that carries the flag (`ExchangeManager.ts:230-235`). This module drops such a request too (`bridge/privacy.go:69-70`), so the P-bit echo plus `applyOutboundPrivacy` (`privacy.go:147-160`) on the unicast reply path is never reached.
- **Correction:** delete the outbound unicast masking, or keep it with a comment that it is unreachable, and cite `ExchangeManager.ts:230-235` instead.
- **Confidence:** confirmed.

## W6-reg-08 — medium — `operational_credentials.go:2254` mirrors a republish matter.js no longer does

- **Register:** `cluster/core/operational_credentials.go:2257-2260` ("Republish the remaining fabric set. Mirrors matter.js Fabric.remove() triggering MdnsServer.reannounceInstance").
- **Claim:** `reannounceInstance` exists nowhere in `packages/protocol/src` or `packages/node/src` at the pin (grep, no hit). On `fabrics.events.deleting`, matter.js only cancels the deleted fabric's advertisements (`DeviceAdvertiser.ts:109-112`) and republishes nothing. The Go hook republishes every remaining fabric after a RemoveFabric. The comment's first citation (`DeviceAdvertiser.ts:84-86`) has drifted to `:109-112`. The comment block at `:2236-2245` also still names the "daemon-side fan-out".
- **Correction:** drop the republish or record it as a divergence, and fix both citations.
- **Confidence:** confirmed.

## W6-reg-09 — low — four open items in the findings register are already fixed

- **Register:** `matter_behaviour_findings.md:608`, `:616`, `:623`, `:708`.
- **Claim:**
  - **`:608`, DataModelTest "nothing calls it at `Node.Start`".** `node/node.go:719` calls `checkDataModelTest` before `br.Start`. `node.Config.GeneralDiagnosticsFeatures` and `MaxPathsPerInvoke` exist (`node/config.go:167-180`).
  - **`:616`, grouped ConfigurationVersion.** `Aggregator.IncreaseConfigurationVersions(keys...)` (`node/aggregator.go:547`) raises once for several devices.
  - **`:623`, CapabilityMinima "not a node option".** `node.Config.CapabilityMinima` exists (`node/config.go:189-195`).
  - **`:708`, TriggerEffect "has no host hook".** `Identify.SetHandler` hands TriggerEffect to the host (`cluster/core/identify.go:137-142, 253-263`, since b3ae3a75).
- **Correction:** remove the four bullets, as the register's own rule says ("closing one removes it").
- **Confidence:** confirmed.

## W6-reg-10 — low — stale names, an internal contradiction and drifted lines in the findings register

- **Register:**
  - `:245` (H3 "PASE note: PBKDFParamRequest tag-5 params are still skipped … deferred");
  - `:320` (`PaseAdapter.SetResponderMRPParams`);
  - `:180-186`, `:202-215`, `:309` ("daemon" wording);
  - `:376-379`, `:307`, `:385-386`.
- **Claim:**
  - **H3's PASE note contradicts `:320`.** The PASE opener stamps the initiator's parameters (`node/security.go:607-610`).
  - **Renamed method.** `SetResponderMRPParams` is now `SetResponderSessionParams` (`bridge/handlers.go:212`).
  - **"Daemon" wording.** The C5, C6 and NodeLabel/Location entries still say "daemon". The wiring is now the node facade's, and it is present: `node/security.go:546,719` `PersistResumption`, `node/commissioning.go:96` `SetPaseVerifierInstaller`, `:151` the CM=2 advertisement, `node/persist.go:165-181`.
  - **Drifted lines.** Keep-alive `bridge/subscribe.go:447-453` → `:454-457`. CountryCode `bridge/fields_reader.go:466-479` → `:228-239`. `general_commissioning.go:688-692` → `:727`. `im/subscription/manager.go:128-149` → `:436-447`.
- **Correction:** one pass: "node facade" for "daemon", the new name, drop the H3 note, and refresh the lines.
- **Confidence:** confirmed.

## W6-reg-11 — low — pin-lifts.md: five "open — settled by the port" verdicts never recorded; the milestone-6 section is stale

- **Register:**
  - `pin-lifts.md:72-73` (#4778, #4777);
  - `:101-102`, `:106` (#4752, #4751, #4788);
  - `:30`, `:54-58`.
- **Claim:**
  - **Unrecorded verdicts.** The five rows wait on phase-16 ports. matterjs_tests.md records those ports: OperationalCredentialsServerTest ported (`:410`), CaseConnectionTimeoutTest ported (`:577`), ServerSubscriptionTest, SustainedSubscriptionTest and ClientEventNotificationTest partial (`:618`, `:687`, `:585`). No row carries the verdict.
  - **Stale row count.** The milestone-6 section says the Ahead register has 30 rows. `make ahead-check` counts 32.
  - **Stale TEVT note.** It says the Thermostat events "have no register row of their own". `BD-chip-Thermostat-Events` exists (`by_design.md` Ahead table, added in 8667fe79, 2026-10-29).
  - **Moved paths (note only).** `controller/discovery/*.go` and `controller/interaction/*.go` are gone (now `client/…`). This is acceptable for a historical table.
- **Correction:** add the verdict (have it / gap) to the five rows from the ported tests, and update the count and the TEVT sentence.
- **Confidence:** confirmed for the missing verdicts. Which verdict each row gets was not decided here.

## W6-reg-12 — low — by_design: 58 drifted citations, removed API names and missing cross-references

- **Register:** many entries; the full list is in the Sonnet maps of this review (`bd-1.md` … `bd-4.md` in the reviewer's scratchpad). The ones a reader trips over:
  - **Removed API names.** `device.WithMeasurement` (`by_design.md:2346`), `device.WithInitial` (`:2281`) and the quoted "(WithFeatures)" error text (`:2207`) name API removed in the BZ wave (`CHANGELOG.md:749`).
  - **Moved package.** `controller/thread` is now `thread/` (`:3538`, `:3727`).
  - **Missing targets.** Cross-references to entries that do not exist: `BD-Matter-AccessControl-Extension-Empty` (`:1318`) and `BD-Matter-48h-UncommissionedWindow` (`:1310`). Files that do not exist: `cluster/wire/admincommissioning.go` holding the 48 h constant (`:1310`, now `bridge/commissioning_window.go:1060`) and `bridge/scenario_tlv_test.go` (`:1366`).
  - **Wrong location.** The per-fabric subscription cap is in `im/subscription/manager.go:62-82`, not `bridge/subscribe.go` (`:1533`).
  - **Removed matter.js files.** `MdnsBroadcaster.ts` (`:1452`) and `MdnsScanner.ts` (`:1440`) are gone. Their logic is in `packages/protocol/src/advertisement/mdns/`.
  - **matter.js line drift.** About 34 matter.js cites have drifted, the behaviour still holding (e.g. `CommandInvokeResponse.ts:446` → `:486`, `SessionManager.ts:490/504` → `:537/:520`, `OccurrenceManager.ts:433` → `:458-462`).
  - **Descriptor TagList.** `L4-TagList` (`:666`) and `BD-Matter-ClosureWithoutTagList` say TagList is "leere Liste / unsupported". `Descriptor.EnableTagList` now exists (`cluster/core/descriptor.go:249-254`), and no composition calls it, so the divergence holds as a default.
- **Correction:** a refresh pass with the four maps as the work list.
- **Confidence:** confirmed for the items quoted (re-read). The remainder is from the delegated maps.

## W6-reg-13 — low — Mirrors comments: 15 drifted lines, 3 wrong files, 2 vanished identifiers (of 60 sampled)

- **Register:**
  - **Wrong file.** `im/dispatcher.go:27` cites `InteractionServer.ts` for the write DataVersion check. It is `AttributeWriteResponse.ts:256`.
  - **Wrong file.** `cluster/core/general_commissioning.go:669` cites `OperationalCredentialsServer.ts` and "recreated on every ArmFailSafe". The creation is `GeneralCommissioningServer.ts:100`, only when not armed (`:90-94` extends).
  - **Wrong file.** `im/client.go:112` cites `valueWriter (TlvAttributeData.ts)`. That name exists at neither commit; the encoding is `Write.ts:165-186`.
  - **Vanished identifiers.** `endpoint/dispatcher.go:165` (`startReadInteraction` / `OnlineContext.forFabricFilteredRead`; now `#executeReadInteraction` :325 with `#prepareOnlineContext` :306-318) and `tlv/encode.go:458` (`TlvUInt64` in `TlvNullable.ts`; generic at `:35-58`).
  - **Drifted lines (15).** For example `bridge/securechannel.go:577` (`DeviceCommissioner.ts:70-72` → `:78-82`), `cluster/core/operational_credentials.go:1898` (`Fabric.ts:557-559` → `:614-616`) and `secure/sigma/protocol.go:425` (`CaseServer.ts:258-264` → `:267-272`).
- **Claim:** In each case the mirrored behaviour is still at the pin; only the address moved.
- **Correction:** fix the comments when the files are next touched. A one-off sweep is cheap for the five named ones.
- **Confidence:** confirmed for the five named ones. The 15 drifts come from the delegated read, of which the reviewer spot-checked two.

## W6-reg-14 — low — a ported matterjs_tests row's Go test still carries a skip that cites a register entry that no longer exists

- **Register:** `notes/parity/matterjs_tests.md:629` (ClientNodeInteractionIcdTest, ported). The skip is at `controller/client/icd_hold_parity_matterjs_test.go:320-322`.
- **Claim:** The subtest "routes a woken LIT read through the icdLit network profile" is `t.Skip`ped: "this module has no network profiles … until milestone 5 phase 25 (notes/parity/matter_behaviour_findings.md, "Network profiles")". Network profiles exist (`client/peer/network.go:43` `NetworkICDLit`). The findings entry is gone. The case is ported elsewhere (`icd_lit_routing_parity_matterjs_test.go`). The register row itself is correct.
- **Correction:** remove the skipped subtest, or point it at the routing test.
- **Confidence:** confirmed.

## W6-reg-15 — low — BD-Matter-BDXInitiatorOverExchange's matter.js premise is not borne out

- **Register:** `notes/parity/by_design.md:3717`.
- **Claim (delegated read, not re-read by the reviewer):** The entry says matter.js's `bdx-session-initiator.ts` "opens the session itself". At the pin `bdxSessionInitiator(messenger: BdxMessenger, config)` (`:33`) takes a caller-built messenger, which is the shape Go's `bdx.Send(ctx, m *Messenger, …)` has.
- **Correction:** re-read and retire the entry if confirmed.
- **Confidence:** plausible.

## W6-reg-16 — note — a by_design paragraph sits detached from its entry

`by_design.md:1832-1839` ("Since brief CT …") follows BD-Matter-DeviceTypeValidationPerTopology's `---` separator. Content verified by the delegated read; only the placement is wrong.

## W6-reg-17 — note — FailsafeCommissioningTest port titles promise more than they assert

`node/failsafe_commissioning_parity_matterjs_test.go:103`, `:132` reuse matter.js's "re-announces …" titles but assert only the rollback and the commissionable record. `matterjs_tests.md:603` marks the row partial and says the re-announcement is missing, so the register is honest; the subtest names are not. This relates to the open item "A closed PASE session does not restart the commissioning advertisement" (`matter_behaviour_findings.md`, BI device side).

## Cross-references to earlier waves (register findings already raised, not counted above)

- W2-sec-02: the defect behind W6-reg-01.
- W2-conc-05: `BD-Matter-CriticalEventBypassMin` (`by_design.md:696`) no longer holds at the pin.
- W2-wire-06: L7-D02 records a TTL the code does not emit.
- W3-clusec-08: the ACL event AdminNodeID sentence (`by_design.md:1801-1810`).
- W2-api-08: the store citation in `by_design.md:691`.
- W1-wire-08: chip TLVReader lines (`by_design.md:957`).

## Checked and sound

- **findings register:**
  - **FIXED / closed entries.** 41 read and verified in the code; 39 sound, plus W6-reg-01 and -02. Sound includes C1–C7, H2, H4–H11 and every Tier-2 FIXED bullet. Their wiring is now the node facade's and present: `SetPeerMRPIntervals` (`node/security.go:543,609,706`), `WithdrawFabric` (`node/node.go:517,597`), event-number ceiling (`node/node.go:370-379`), NodeLabel persistence, `recordPaseFailure`/`paseMaxErrors`, `ExpediteDue`, `NewCounter` 28-bit seed, `enforceKeySetBudget`, `endpointHasDeviceType`, `readAuthorizedResults`.
  - **Open items.** 55: 25 checked against the code are still open (keep-alive suppress, subscription persisted on session close, no BUSY, `candidatesOf`, no `assertedAtRoot`, known-answer TTL, `StatusError` origin, empty ICAC, group sender node id, CSR CN, WildcardPathFlags, write path without cluster, CountryCode, Basic window command, 900 s fail-safe, TagList, monotonic clock, Switch clock, primaries, Wi-Fi fail-safe fabric, and others); 4 are fixed (W6-reg-09); 26 read, not verified in the code.
  - **"Verified sound" list.** Read; not re-verified.
- **by_design:**
  - **Ahead register.** 32/32 markers hold at the pin (`make ahead-check`). Every Behaviour-source chip path exists at `6170af84`, except `BD-chip-Thermostat-Schedules`. That row cites `645b267a` for a schedules server the harness commit lacks, and the entry states this itself (`:4672-4677`); sound.
  - **`###` entries.** 274: about 215 with no problem, 22 false (W6-reg-03, -04, -06), plus drift.
  - **Audit table.** :652-712, 30 rows read: 3 false (L3-PFAD-6, L6-PFAD-3, TimeSync-NotMounted). Host-path rows are left as the header says.
  - **"Removed" section.** Sound: `cluster/core/power_source.go` is absent, and `measurement.PowerSourceServer` is wired.
- **pin-lifts:** 31 rows. Every "gap → register" row is either still in the register or fixed (for example #4775 KeypadLockout, `cluster/thermo/thermostatuserinterfaceconfiguration.go:34-38`). The verdicts are sound as a record at the lift, apart from W6-reg-11.
- **controller_profiles:** 1 row; the generated `surface/profiles_gen.go:17-22` matches, and MSCH is bit 7 = 0x80. Sound.
- **matterjs_tests:** 396 rows (delegated): no ported row names a missing Go test, every matter.js file exists at the pin, and 63 `File.ts:line` cites are in range. `go test -run Testparity ./script/testparity/` passes. Only W6-reg-14.
- **Mirrors (60):** 39 OK, 1 cite without a file (`contract/matter.go:197`, a glob, acceptable), 18 drift / wrong file (W6-reg-13), 2 behaviour gone (W6-reg-07, -08).

## Not read

- The remaining 846 `Mirrors matter.js` comments.
- 26 open items of the findings register, beyond reading the entry text (the Groupcast, field-conformance, request/response-constraint, ACL-interaction, OccupancySensing, concentration, TCP, event-store, key-set-0, ExpiringUser, Presets-staging, ColorControl-quiesce, Wi-Fi-persistence and Wi-Fi-SSID items).
- H8's "known limitation" (the stateless chunk dispatch) against the `writeTxs` table that now exists (`bridge/exchange_routing.go:62-69`). It may be stale.
- The chip line ranges inside the BD-chip rule lists, beyond the delegated spot checks.
- The `by_design.md` "Parity Run" tables at :1222-1264 and the L4/L10/L5 tables at :743-785.
- Whether each ported matterjs_tests row covers every matter.js `it` (BtpSessionHandlerTest and BtpCodecTest are the two the map flags for a count).
