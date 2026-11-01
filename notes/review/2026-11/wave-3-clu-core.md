# Wave 3 review — theme `clu-core` (non-security servers)

- **Scope:** the hand-written, non-security servers in `cluster/core/`: `descriptor.go`, `basic_information.go`, `bridged_device_basic_information.go`, `fixed_label.go`, `user_label.go`, `identify.go`, `actions.go`, `scenes_management.go`, `scenes_attributes.go`, `localization_configuration.go`, `time_format_localization.go`, `unit_localization.go`, `power_source_configuration.go`, `general_diagnostics.go`, `software_diagnostics.go`, `ethernet_network_diagnostics.go`, `wifi_network_diagnostics.go`, `diagnostic_logs.go`, `messages.go`, `ecosystem_information.go`, `ahead.go`, `definitions.go`, `doc.go`, `limits.go`; `cluster/wire/` (inventory of every file, the decoders of `levelcontrol.go`, `onoff.go`, `doorlock.go`, the `scenes_management.go` stub, `doc.go`). The security clusters of `cluster/core` and `cluster/wire/admincommissioning.go` belong to the other W3 reviewer and were not read. There is no `thread_*diagnostics*.go` and no `power_source.go` in `cluster/core` (the brief's globs and the comment at `schema/parity_test.go:54` name files that do not exist).
- **HEAD:** go-fabric `769ea3c2` (2026-11-01).
- **Pins:** matter.js `862ad75f` (checkout at `../matter.js` matches). connectedhomeip: the checkout is `57f29895`; every chip line below was read with `git show 6170af84:<path>` (`actions-server/ActionsCluster.cpp`, `messages-server/messages-server.cpp`, `scenes-server/ScenesManagementCluster.cpp`).
- **Method:** read slices only; no code modified. Severity as in W1/W2: high = security or panic reachable from the network; medium = wire-visible or host-visible divergence from both references; low = divergence with bounded impact; note = provenance, coverage, dead code. `notes/parity/by_design.md` was checked before every call: the rows cited inline cover what they cover and nothing below contradicts an existing row.
- **Totals:** high 0 · medium 3 · low 6 · note 5.

---

## W3-clucore-01 — BridgedDeviceBasicInformation answers a NodeLabel over 32 bytes with FAILURE, not CONSTRAINT_ERROR

- **Severity:** medium · **Confidence:** confirmed
- **Where:** `cluster/core/bridged_device_basic_information.go:457-463` — `value.(string)` failing and `len(s) > 32` both return a plain `errors.New` / `fmt.Errorf`; the dispatcher's `classifyError` (`endpoint/dispatcher.go:822-839`) maps any error that is not a `StatusCodeError` to `im.StatusFailure`. The dispatcher runs no constraint check of its own before `MatterWrite` (no `CheckValue` call site outside `cluster/`; `endpoint/dispatcher.go:796-806`). The root `BasicInformation` does it right: `checkLabelWrite` (`basic_information.go:815-820`) runs `inst().CheckValue` and wraps the result in `refusedWrite`, which carries CONSTRAINT_ERROR. The only test, `bridged_device_basic_information_test.go:156-163`, asserts `err != nil` and not the status.
- **Claim:** `chip-tool bridgeddevicebasicinformation write node-label "<33 ASCII bytes>" <node> <bridged-endpoint>` (or the same WriteRequest from any controller) is answered StatusIB FAILURE (0x01); both references answer CONSTRAINT_ERROR (0x87). TC-BRBINFO-2.1's negative NodeLabel step reads the status.
- **matter.js:** `packages/model/src/standard/elements/bridged-device-basic-information.element.ts:30` — NodeLabel is the BasicInformation attribute by reference, whose constraint is `basic-information.element.ts:36` `"max 32"`; the write validator `packages/node/src/behavior/state/validation/constraint.ts:199-211` throws a `ConstraintError` ("String length of … is not within bounds"), the citation the repo's own `checkLabelWrite` comment uses for the root cluster.
- **Fix:** replace the two hand checks with the root cluster's shape — `if _, err := b.inst().CheckValue(attrID, s, nil); err != nil { return refusedWrite{…, status: err} }` — and make the test assert `im.StatusConstraintError`; the wrong-type arm can stay FAILURE (matter.js answers an `UnexpectedDataError` the same way).

## W3-clucore-02 — An IdentifyTime attribute write never reaches the host's IdentifyHandler; only the Identify command does

- **Severity:** medium · **Confidence:** confirmed
- **Where:** `cluster/core/identify.go:211-228` (`MatterWrite`: coerce, store, start the countdown, bump — no `hostHandler()` call) against `:231-250` (`MatterInvoke` Identify: `h.MatterIdentify(ctx, t)` first). The countdown goroutine (`:299-330`) also ends silently when it reaches zero: the host that started blinking on `MatterIdentify(10)` is never told to stop except by its own timer. `contract/identify.go:8-28` documents the handler as "the controller's Identify and TriggerEffect commands".
- **Claim:** `chip-tool identify write identify-time 10 <node> <ep>` makes IdentifyTime count down on the wire while the bridged device does nothing; `chip-tool identify identify 10 …` on the same endpoint blinks. matter.js treats both entry points identically, and the spec's IdentifyTime is "writable" precisely so a controller can identify through the attribute (the repo's own comment at `:208-210` quotes it).
- **matter.js:** `packages/node/src/behaviors/identify/IdentifyServer.ts:57-58` — `// So whenever the attribute OR the identify command was invoked we react to it.` / `this.reactTo(this.events.identifyTime$Changed, this.#identifyTimeChangedHandler)`; the handler starts or stops identifying on the new value (`:91-112`) and `#identifyTick` (`:121-127`) drives the same reactor to `stopIdentifying` when it reaches zero.
- **Fix:** in `MatterWrite`, after `coerceUint16`, run the host handler exactly as the command arm does (an error refuses the write with its status, as it refuses the command); when the countdown stores zero, call the handler once with `identifyTime == 0` (the contract already defines 0 as "stops"), so the host sees matter.js's `stopIdentifying`. Add a parity case per entry point.

## W3-clucore-03 — AddScene silently truncates a SceneName longer than 16 characters; both references answer CONSTRAINT_ERROR

- **Severity:** medium · **Confidence:** confirmed
- **Where:** `cluster/core/scenes_management.go:697-701` — a name longer than `scenesMaxNameLength` is cut to 16 UTF-16 units and the scene is added with status SUCCESS. The bridge's hand decoder deliberately leaves the field unbounded: `bridge/fields_reader_scenes.go:15-17` "The server relaxes SceneId, TransitionTime and SceneName constraints and answers them in its responses (ScenesManagementServer.ts schema extension)". SceneId and TransitionTime are in fact answered CONSTRAINT_ERROR by the server (`:684-687`); only SceneName is relaxed.
- **Claim:** `chip-tool scenesmanagement add-scene 1 1 1000 "seventeen-chars-x" '[]' <node> <ep>` returns AddSceneResponse status SUCCESS with the scene stored under a different name than the one sent; a following ViewScene returns the truncated name. Both references answer AddSceneResponse status CONSTRAINT_ERROR and store nothing.
- **matter.js:** the schema extension relaxes only the *echoed response* field — `ScenesManagementServer.ts:64-73` `constraintErrorWithSceneId` sets `Supervision(response, "sceneId").constraint = false` "to suppress constraint validation on the echoed field". The request is validated unchanged: `scenes-management.element.ts:43` AddScene `SceneName … constraint: "max 16"`, and `packages/protocol/src/action/server/CommandInvokeResponse.ts:486` `requestTlv.validate(request)` turns a `ValidationError` into the invoke's status (`:529-533`; a `ValidationError` is CONSTRAINT_ERROR, `packages/types/src/common/ValidationError.ts:16-29` as cited in W2-wire-03).
- **chip:** `src/app/clusters/scenes-server/ScenesManagementCluster.cpp:536-539` at `6170af84` — `if (req.transitionTime > kScenesMaxTransitionTime || req.sceneName.size() > kSceneNameMaxLength || …) { response.status = Status::ConstraintError; …}`.
- **Fix:** in `addScene`, answer `resp.Status = im.StatusConstraintError` for `len(utf16.Encode([]rune(name))) > scenesMaxNameLength` alongside the SceneID/TransitionTime check and delete the truncation; correct the decoder comment. One parity case with a 17-character name.

## W3-clucore-04 — An IdentifyTime write above 65535 is masked to a different valid value

- **Severity:** low · **Confidence:** confirmed
- **Where:** `cluster/core/identify.go:365-390` `coerceUint16`: `uint32` and `uint64` arms return `uint16(x & 0xFFFF)`; a primitive write reaches the server as `el.Uint` (`uint64`) from `bridge/attribute_value_reader.go:441-468` `primitiveAttributeValue`, and nothing between the decoder and `MatterWrite` holds the value to the attribute's type (`endpoint/dispatcher.go:796-806`).
- **Claim:** a WriteRequest with IdentifyTime encoded as a 4-byte unsigned 70000 is accepted with SUCCESS and the countdown starts at 4464. matter.js refuses it with CONSTRAINT_ERROR (`TlvUInt16` out of bounds → `ValidationOutOfBoundsError`, `packages/types/src/tlv/TlvNumber.ts:151-155`, a `ValidationError`). The same mask is in the W2 register for path fields (W2-wire-03); this is the attribute-value side of it.
- **Fix:** have `coerceUint16` refuse `x > math.MaxUint16` with a `StatusCodeError` CONSTRAINT_ERROR (or run `identifyInst.CheckValue(identifyAttrTime, …)` before storing); keep the masking only for the Identify command's generated request, which is already bounded by its decoder.

## W3-clucore-05 — The Identify countdown reaching zero is never reported to subscribers and bumps no DataVersion

- **Severity:** low · **Confidence:** confirmed (code); subscription consequence plausible
- **Where:** `cluster/core/identify.go:299-330` — the ticker goroutine stores the atomic only; `Identify` implements no `contract.ChangeNotifier` / `AttributeChangeNotifier` and does not bump `dataVersion` on a tick. Dirty-marking of IdentifyTime comes solely from the write/invoke path (`bridge/receive_dispatch.go:674-687` `reportWrittenAttributes`), so a subscriber sees the value a controller set and never the zero.
- **Claim:** a controller subscribed to IdentifyTime after `identify 5` receives 5 and then nothing; a read ten seconds later returns 0. matter.js keeps the per-second ticks quiet but reports the edges: `IdentifyServer.ts:41-46` `shouldEmit: … hasRemoteActor(context) || ((oldValue === 0 || newValue === 0) && newValue !== oldValue) ? "now" : false`.
- **Fix:** give `Identify` an `OnAttributeChange` (the `cluster.AttributeChanges` helper `ScenesManagement` and `Messages` use) that fires IdentifyTime once when the countdown stores zero, and bump `dataVersion` there; the bridge's existing notifier wiring (`bridge/subscribe.go:1300-1330`) delivers it. A test with the fake clock: tick to zero, expect one change.

## W3-clucore-06 — GeneralDiagnostics NetworkInterfaces: the synthetic fallback carries the all-zero MAC matter.js filters out, and the list is capped at 4 instead of 8

- **Severity:** low · **Confidence:** confirmed (code); field impact plausible
- **Where:** `cluster/core/general_diagnostics.go:1037-1046` `syntheticEthernetIface` (`HardwareAddress: make([]byte, 6)`), used at `:987` and `:1024-1030` when no interface qualifies; `:967` `maxIfacesForApple = 4` with the loop break at `:1021-1023`.
- **Claim:** a container host with only a loopback (or MAC-less) interface serves one NetworkInterface entry `eth0` with `00:00:00:00:00:00`; a host with five qualifying interfaces serves four. matter.js serves the interfaces the OS has, minus those with the unassigned MAC, up to the spec's eight.
- **matter.js:** `packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:430-453` — `.filter(({ mac }) => mac !== "00:00:00:00:00:00")`, `.slice(0, 8)`, `name.substring(0, 32)`; the element `general-diagnostics.element.ts:27` NetworkInterfaces `constraint: "max 8"`.
- **Fix:** lift the cap to 8 (the constraint) and either drop the all-zero fallback (serve `[]`, as matter.js would on such a host) or record the Apple-driven fallback as a `by_design.md` row — the comment's "Apple needs one entry" is an interop claim with no citation, and no existing row carries it.

## W3-clucore-07 — `wire.ScenesManagement` is an unused public stub that serves a non-conformant cluster next to `core.ScenesManagement`

- **Severity:** low · **Confidence:** confirmed
- **Where:** `cluster/wire/scenes_management.go:1-45` — SceneTableSize 0, FabricSceneInfo `[]any{}`, FeatureMap 0, every command UNSUPPORTED_COMMAND ("HM has no scene management"); no non-test reference in the module (`grep -rn "wire\.ScenesManagement"` is empty outside tests). `core.ScenesManagement` (`cluster/core/scenes_management.go`) is the real server the assembler mounts (`endpoint/materialize.go:522-534`).
- **Claim:** a host that mounts the exported stub (it satisfies `contract.ClusterServer`) advertises 0x0062 with its eight mandatory commands unsupported; the HM wording is a Homematic leftover that contradicts `contract/`'s "no CCU wire knowledge enters this module". Dead public API is also an apidiff surface the W5 reviewer will meet.
- **Fix:** remove the file (owner rule 2026-10-21: replaced paths go at once with a migration-guide line); `core.ScenesManagement` is the one shape.

## W3-clucore-08 — The hand-written `cluster/wire` command decoders mask field widths instead of refusing out-of-range values

- **Severity:** low · **Confidence:** confirmed (code); reachability plausible-not
- **Where:** `cluster/wire/levelcontrol.go` `DecodeMoveToLevel` (`uint8(el.Uint & 0xFF)` for Level, `uint16(el.Uint & 0xFFFF)` for TransitionTime), `cluster/wire/onoff.go` `DecodeOffWithEffect` / `DecodeOnWithTimedOff` (same masks), `cluster/wire/doorlock.go` `DecodeDoorLockRequest` (any octet length accepted). They are reached only through the `[]byte` arm of the server's request coercion (`cluster/levelcontrol/levelcontrol_server.go:939-944`); the bridge decodes live invokes with the generated, bounded decoders (`cluster/spec/codec.go:102,197-260` `DecodeUintIn` / `DecodeStringIn` / `DecodeBytesIn` answer CONSTRAINT_ERROR).
- **Claim:** a host handing raw TLV bytes to the server sees Level 300 applied as 44 with SUCCESS. `BD-Matter-TLVPermissive` (`by_design.md:354-398`) justifies the *absent-field* leniency these decoders keep for Google Home; it says nothing about value masking, which neither reference does.
- **Fix:** bound-check in `walkContext`'s visitors and return the package sentinel (the server maps it to INVALID_COMMAND) — or, since the bridge no longer needs the raw arm, delete the `[]byte` path and the decoders with the migration line. Either way one line in the by_design row saying which leniency is intended.

## W3-clucore-09 — Messages.PresentMessagesRequest skips the MessageControl / feature couplings chip enforces

- **Severity:** low · **Confidence:** confirmed
- **Where:** `cluster/core/messages.go` `present` — the only request rule is "Responses without RESP → INVALID_COMMAND"; the generated decoder bounds MessageId (16), MessageText (256) and Responses (4) (`cluster/spec/messages/definition_gen.go:192-198`). Not checked: a response option with `MessageResponseID` 0 or a missing label, duplicate response ids, and the MessageControl bits that chip couples to the SPKM/AUDM features and their fields.
- **Claim:** `PresentMessagesRequest` with `Responses: [{MessageResponseID: 0, Label: "x"}]` on a RESP server is queued with SUCCESS; chip answers INVALID_COMMAND. matter.js has no Messages logic (`MessagesServer.ts`, an empty subclass, 14 lines at the pin), so chip is the authority (CLAUDE.md order 2); the register row `BD-chip-Messages-CoreHeld` (`by_design.md:3117-3131`) names only the RESP rule and so does not cover this gap.
- **chip:** `src/app/clusters/messages-server/messages-server.cpp` at `6170af84`, the validation block at `:280-385`: `messageId.size() == kMessageIdLength` and `messageText.size() <= kMessageTextLengthMax` → ConstraintError (covered here by the decoder); SpokenMessage/AudioMessage bits without the feature → InvalidCommand, without languageCode/messageUri → ConstraintError; `size <= kMessageMaxOptionCount` → ConstraintError; each response needs `messageResponseID` and `label` → InvalidCommand, and `messageResponseID >= kMessageResponseIdMin` (1).
- **Fix:** add the response-option rules (id ≥ 1, label present, ids unique → INVALID_COMMAND) to `present`, gate the MessageControl bits on the server's features, and extend the by_design row with the lines above; one negative case per rule in the Messages parity test.

## W3-clucore-10 — BasicInformation's LocalConfigDisabled write arm is unreachable and its comment wrong

- **Severity:** note · **Confidence:** confirmed
- **Where:** `cluster/core/basic_information.go:792-799` accepts the write and bumps DataVersion ("accept the write but discard the value"), but the attribute is not in `instanceOptions` (`:366-377`) and `MatterRead` answers unsupported (`:681-682`); the dispatcher refuses a write to an unlisted attribute with UNSUPPORTED_ATTRIBUTE before `MatterWrite` runs (`endpoint/dispatcher.go:295-299`). matter.js leaves the "O" attribute off by default as well (`basic-information.element.ts:79`; `BasicInformationServer.ts` sets no default for it), so the served surface is right; only the arm is dead.
- **Fix:** delete the arm (the definition's `ValidateWrite` fall-through already answers UNSUPPORTED_ATTRIBUTE), or serve the attribute properly if a host ever needs it.

## W3-clucore-11 — GeneralDiagnostics: MatterReportable lists an attribute MatterRead does not serve; RebootCount's unseeded default differs from matter.js

- **Severity:** note · **Confidence:** confirmed
- **Where:** `cluster/core/general_diagnostics.go:866-868` returns `{UpTime, BootReason}` while `:489-490` answers BootReason `(nil, false)`; harmless today because the server is no change notifier, but any future notifier wiring would dirty-mark an unsupported path. `:467-471` serves RebootCount 1 when `SetPersistedCounters` was never called; matter.js initialises it to 0 and increments on every later boot (`GeneralDiagnosticsServer.ts:102-105`).
- **Fix:** make `MatterReportable` `{UpTime, RebootCount, TotalOperationalHours}` (the ones that move) and unseeded RebootCount 0; the `MatterReportable ⊆ MatterAttributes` guard of W3-clucore-13 catches the first class of drift for every server.

## W3-clucore-12 — Descriptor: reportable set and DataVersion digest cover PartsList/ServerList only

- **Severity:** note · **Confidence:** confirmed
- **Where:** `cluster/core/descriptor.go:317-319` `MatterReportable` is PartsList alone; `:60-73` derives the DataVersion from ServerList + PartsList, so a TagList change (`EnableTagList` / `AddTags`, `:253-287`) moves neither the version nor a subscription, and a ServerList change relies on the bridge's own dirty-marking (`BD-Matter-SurfaceChange-ConfigurationVersion`). matter.js's state-backed attributes all report on change (`DescriptorServer.ts:170-180` ServersChanged writes `serverList`; `Datasource` versions every write).
- **Fix:** include DeviceTypeList, ServerList and (with the feature) TagList in `MatterReportable`, and fold the tag list into the version digest; or state in the type comment that tags are set before serving only.

## W3-clucore-13 — `TestParityCodeMatchesGeneratedSchema` no longer pins anything the servers can drift on

- **Severity:** note · **Confidence:** confirmed
- **Where:** `schema/parity_test.go:32-64` compares each server's revision constant with the schema; since ADR 0013 every such constant is an alias of the generated definition (`descriptor.go:88`, `bridged_device_basic_information.go:90`, `identify.go:103`, …), so the comparison is tautological, and its source map still names files that do not exist (`core/power_source.go`, `model/custom/...`). Nothing pins per server: FeatureMap and the attribute/command/event sets a server *serves* against what it *answers* (W3-clucore-11 is exactly that), `MatterReportable ⊆ MatterAttributes`, or that every id in `MatterAttributes` is answered by `MatterRead` (the Apple cache-drop class the comments keep citing).
- **Fix:** one table-driven guard in `cluster/core` over every constructor: for each server assert `MatterReportable ⊆ MatterAttributes`, every `MatterAttributes` id answers `MatterRead` with `ok`, `MatterAcceptedCommands` equals what `MatterInvoke` does not answer UNSUPPORTED_COMMAND for, and FeatureMap equals the bound instance's; retire the revision table or regenerate its source map.

## W3-clucore-14 — ScenesManagement built without `Fabrics` serves an empty FabricSceneInfo even to the accessing fabric

- **Severity:** note · **Confidence:** confirmed
- **Where:** `cluster/core/scenes_management.go:458-472` enumerates `s.cfg.Fabrics(ctx)` and nothing else; with a nil `Fabrics` the mandatory list is always empty. The assembler wires it to the fabric store (`endpoint/materialize.go:530-533`, `groups/manager.go:532-543` `StoredFabrics` = every commissioned fabric), so the bridge is right; a host constructing the server directly through the exported `ScenesConfig` is not.
- **Fix:** when `Fabrics` is nil, fall back to the accessing fabric of the request (`im.FabricFilterFromContext`) so the accessing fabric's own entry is always served, as matter.js's fabric-scoped state is.

---

## Checked and sound

- Descriptor PartsList composition: root = every non-root endpoint (full family), aggregator = its bridged children, both sorted (`node/root.go:532-559`) — matches `DescriptorServer.ts:224-258` and `DeviceTypeModel.ts:33-46`.
- Descriptor DeviceTypeList dedupe keeps the first revision (`descriptor.go:124-139` ↔ `DescriptorServer.ts:91-111`); `AddTags` dedupe and label replacement (`:253-287` ↔ `:122-147`); TagList served and listed only with the feature; writes answered UNSUPPORTED_WRITE / UNSUPPORTED_ATTRIBUTE by the definition.
- BridgedDeviceBasicInformation: Reachable + UniqueID mandatory, every other attribute served only when set (`:234-262`, `:331-425`), ReachableChanged priority taken from the definition (info, `bridged-device-basic-information.element.ts:55`), ConfigurationVersion default 1, SerialNumber backfill documented.
- BasicInformation: NodeLabel "max 32" and Location "2" answered CONSTRAINT_ERROR via `CheckValue` (`:815-820`); StartUp/ShutDown/Leave/ReachableChanged priorities from the definition; ConfigurationVersion never 0 and never decreasing (`:1057-1080` ↔ `BasicInformationServer.ts:103-118`); DataModelRevision default 21.
- GeneralDiagnostics TestEventTrigger: the enable key is never readable (TestEventTriggersEnabled is the bool only); all-zero or mismatching key → CONSTRAINT_ERROR, unsupported trigger → INVALID_COMMAND, constant-time compare (`:645-700` ↔ `GeneralDiagnosticsServer.ts:129-146`); PayloadTestRequest statuses (`:703-731` ↔ `:163-201`); TimeSnapshot PosixTimeMs null without a UTC source; TotalOperationalHours floored over base + session.
- Actions: unknown action → NOT_FOUND, unsupported command bit → INVALID_COMMAND, lists bounded to 256 (`actions.go` ↔ chip `ActionsCluster.cpp:34-35,120,141`); matter.js has no logic (`ActionsServer.ts`, 14 lines).
- ScenesManagement: per-fabric capacity `min((size-1)/2, 253)` and the shared-table bound (`BD-Matter-Scenes-RemainingCapacity`); SceneID > 254 / TransitionTime > 60 000 000 → CONSTRAINT_ERROR, unknown group → INVALID_COMMAND, missing scene → NOT_FOUND, full table → RESOURCE_EXHAUSTED; StoreScene keeps name and transition of an existing entry; RecallScene's TransitionTime override; unknown EFS cluster skipped, unknown attribute → INVALID_COMMAND, last duplicate wins (`scenes_attributes.go:92-120` ↔ `ScenesManagementServer.ts` `#decodeExtensionFieldSets`); fabric removal and group removal clean the table and the current-scene validity; FabricSceneInfo fabric-filtered and limited to commissioned fabrics; no accessing fabric → UNSUPPORTED_ACCESS.
- Messages: queue of eight → RESOURCE_EXHAUSTED, Messages list fabric-filtered, cancel touches the accessing fabric only, MessageComplete/Presented/Queued events with the definition's priorities; MessageId 16 / MessageText 256 / Responses 4 enforced by the generated decoder (`BD-chip-Messages-CoreHeld`).
- LocalizationConfiguration / TimeFormatLocalization: ActiveLocale / ActiveCalendarType held to `in supportedLocales` / `in supportedCalendarTypes` by `spec.Server.checkIn` (`cluster/spec/server.go:238-247`, `definition_gen.go:46` / `:124`); matter.js's servers only set defaults. UnitLocalization: `BD-chip-UnitLocalization-TemperatureUnitInSupported`.
- UserLabel: beyond the limit → RESOURCE_EXHAUSTED (`UserLabelServer.ts:22-24`); FixedLabel, PowerSourceConfiguration (Sources bounded to 6), SoftwareDiagnostics (Watermarks feature gates ResetWatermarks), Ethernet/WiFi diagnostics (nullable reads, ResetCounts, the three WiFi events) are thin generated servers; matter.js has no logic for them.
- DiagnosticLogs: `BD-chip-DiagLogs-BDX` holds; intent/protocol outside the enum → INVALID_COMMAND, BDX without designator → INVALID_COMMAND, designator > 32 → CONSTRAINT_ERROR, one transfer at a time (Busy), quiesce waits for it.
- EcosystemInformation: fabric-sensitive entries redacted to FabricIndex on an unfiltered read, dropped on a filtered one, every entry redacted on a PASE session (`:147-200`).
- Identify TriggerEffect accepted everywhere: `BD-Matter-Identify-TriggerEffectEverywhere` (`by_design.md:3826`).
- `limits.go`, `ahead.go`, `definitions.go` (`refusedWrite` never answers FAILURE for a definition-judged refusal), `doc.go`.

## Not read

- Security clusters of `cluster/core` and `cluster/wire/admincommissioning.go` (other reviewer).
- `cluster/core/basic_information.go:300-365, 400-560, 860-940` (version-string helpers, UniqueID derivation, OTA node info); `general_diagnostics.go:1-247, 570-612` (type block, uptime arithmetic); `diagnostic_logs.go:1-392, 606-680` (collector plumbing, request decoder); `ecosystem_information.go:200-324` (device validation, setters); `messages.go` `RemoveFabric` tail; `scenes_attributes.go:130-290` (value encode/decode helpers).
- `cluster/wire/thermostat.go`, `windowcovering.go`, `colorcontrol.go`, `operationalstate.go`, `schedules.go`, `modebase.go`, `genericswitch.go`, `fancontrol.go`, `events.go`, `closurecontrol.go` beyond their type/function inventory (they belong to the generated-tranche reviewers' clusters).
- The chip-tool/CSA YAML cases for these clusters (`internal/chiptool/`) — W6.
