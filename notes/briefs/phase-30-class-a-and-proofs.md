# Brief: phase 30 — the four class-(a) defects and the three real-controller proofs

Milestone 8 (Proof), phase 30 (`docs/roadmap.md:361`; concept v13 §C.1).
Branch `m8/phase-30` from `main` after wave 29b-2 landed. One PR, title
"chiptool, cluster/core, endpoint: the four class-(a) defects closed —
Wi-Fi networks persist and reconnect, TC-EEVSE-2.3 answers
RESOURCE_EXHAUSTED — and the group, subscription and timed-write proofs
with chip-tool (phase 30)". CHANGELOG under `[Unreleased]` → Added /
Changed / Fixed. No release.

Owner decisions 2026-11-03 (wizard): (1) TC-EEVSE-2.3 **deviates** from
matter.js: the server answers RESOURCE_EXHAUSTED before the decoder's
`max 10` CONSTRAINT_ERROR; (2) the committed Wi-Fi network is persisted in
`store.Settings` and reconnected on start, chip as the authority; (3) the
per-attribute timed-write enforcement point is built now with a parity
test; (4) one chip-tool binary leg each for group messaging and
subscription re-establishment. The read map
`~/.cache/go-fabric-scratch/read-phase30.md` has `file:line` for every
source (§2 CNET, §3 EEVSE, §4 group, §5 subscription, §6 timed); it is a
map — re-read every line you cite at 862ad75f (`../matter.js`) and
6170af84 (`../connectedhomeip`, `git show 6170af84:<path>`). The lines
below were re-read by the reviewer.

## Deliverables

1. **TC-EEVSE-2.3: the server judges the eleventh target.** Today the
   generated decoder refuses `ChargingTargetScheduleStruct.ChargingTargets`
   at its `max 10` (`cluster/spec/energyevse/definition_gen.go:94,109`,
   `spec.DecodeListIn` → `outOfRange` → CONSTRAINT_ERROR,
   `cluster/spec/codec.go:101-103,255-273`) before the server's own check
   (`cluster/energy/evse.go:1236-1237`, `evseMaxTargetsPerDay` `:101`,
   mirrors chip `EnergyEvseCluster.cpp:564-565`) can answer, exactly as
   matter.js does (`CommandInvokeResponse.ts:484-487` validates before the
   invoker; `TlvArray.ts:111-113`; `energy-evse-cluster.element.ts:254`).
   chip's case wants RESOURCE_EXHAUSTED (`src/python_testing/TC_EEVSE_2_3.py:411-438`,
   step 19). Extend the generator's `relaxedRequestFields`
   (`script/clustergen/relaxed.go`, applied in `gen.go:1004-1014`) so it
   also keys a **struct** field (`Cluster.Struct.Field`, here
   `EnergyEvse.ChargingTargetScheduleStruct.ChargingTargets`) and so the
   generated comment can name a chip line and an owner decision instead of
   a matter.js relaxation (a second map or a value struct `{at, why}`;
   keep the existing two IcdManagement entries unchanged). Regenerate
   (`go run ./script/clustergen`); `go test ./script/clustergen/...` must
   stay green with the committed definition. The server's check at
   `evse.go:1236-1237` then answers on the wire; add the wire-level test
   (through `bridge/fields_definition.go` `decodeByDefinition`, eleven
   targets → RESOURCE_EXHAUSTED, ten → the server's path) beside
   `cluster/energy/evse_test.go:345-358`. Registers: a new by_design row
   `BD-chip-EVSE-TargetsServerHeld` (one paragraph: matter.js answers
   CONSTRAINT_ERROR by model validation, chip's handler answers
   RESOURCE_EXHAUSTED, the CSA case expects chip's, owner decision
   2026-11-03) in the *Ahead of matter.js* register with the matter.js
   marker `CommandInvokeResponse.ts:485-487` + `EnergyEvseServer.ts` (a
   stub) and the chip source; `BD-chip-Harness-ModelOverCase`
   (`by_design.md:3387-3401`) loses its TC-EEVSE-2.3 sentence (the other
   three subjects stay); `by_design.md:1870` and
   `matter_behaviour_findings.md:798` mentions updated. The harness row
   `internal/chiptool/families_table_test.go:707` goes; the case runs and
   its counts enter the golden.
2. **CNET-4.1/4.4/4.9: the committed network persists and reconnects.**
   `cluster/core/network_commissioning_wifi.go` keeps `networks` /
   `committed` in memory (`:36-60`); `OnCommissioningComplete` `:573-577`
   is chip's `CommitConfiguration` (`NetworkCommissioningWiFiDriver.cpp:135-170`,
   KVS put of SSID + credentials), `OnFailSafeExpired` `:579-591` the
   revert. Add `Store store.Settings` to `WiFiNetworkCommissioningConfig`
   (JSON of the committed list, SSID + credentials, as TimeSynchronization
   does: `time_synchronization_features.go:67-70,175-200`), saved in
   `OnCommissioningComplete`, loaded in the constructor (`:92-106`) into
   `networks` and `committed`; wired from `node/root.go:276-297` with
   `d.st` as `:585-600` does for TimeSynchronization. **Reconnect on
   start:** chip's `Init` nulls LastNetworkingStatus / LastNetworkID
   (`NetworkCommissioningCluster.cpp:200-215`) and the Linux driver's
   `Init` (`WiFiDriver.cpp:74-118`) connects to the saved network; our
   server re-issues the driver's `ConnectNetwork` for the loaded network
   when the node starts (a `Start(ctx)` hook the root calls after the
   store is open; read `node/root.go` for the start order), setting
   `connected`, LastNetworkingStatus Success and LastNetworkID as `connect()`
   `:525-570` does — which TC-CNET-4.1 steps 7–8 read (`TC_CNET_4_1.py:152-170`).
   The simulated driver (`examples/reference-bridge/wifisim.go:46-56`)
   answers an **undirected** scan with the networks it knows and
   NetworkNotFound for an unknown SSID (TC-CNET-4.4: `TC_CNET_4_4.py`
   `scan_and_check` `:74-122`, the random-SSID scan `:133-134`); give it
   the SSID list the findings entry names (`--wifi-sim-ssid`, default
   `zigbeehome`, the snapshot's SSID in `families_table_test.go:125-136`),
   which also moves TC-CNET-4.23 out of class (c). Tests:
   `cluster/core/network_commissioning_wifi_test.go` gains persist / load
   / reconnect / revert cases; `wifisim_test.go` the scan rules. Registers:
   findings `:727-734` and `:735-739` removed; `BD-chip-WiFiNetworkCommissioningServer`
   (`by_design.md:3484-3501`) gains the persistence sentence; an *Ahead
   of matter.js* row (matter.js `NetworkCommissioningServer.ts:7-18` is a
   stub). Harness rows `families_table_test.go:346-348` go; the three
   cases run from the snapshot (`chipcases_test.go:537-541` provisions and
   stops the daemon; the store file carries the network).
3. **Per-attribute timed writes enforced.** The write path has no
   per-attribute check (`schema.IsTimedWrite` `schema/timed.go:45-48` is
   unused on the server). Build it where the invoke path has its rule
   (`im/invoke.go:773-777`, after access and fabric checks): the write
   context learns the timed window as the invoke context does
   (`bridge/receive_dispatch.go:401` `checkTimedGate`, `:561`
   `im.WithTimedInteraction` — add the same to `dispatchWriteRequest`),
   and `endpoint/dispatcher.go`'s write loop answers NEEDS_TIMED_INTERACTION
   for a concrete path whose attribute is timed in an untimed interaction,
   after the writability verdict and the ACL gate, as matter.js
   `AttributeWriteResponse.ts:243-247` (wildcard: silently skipped,
   `:343-345`; `AccessControl.ts:290-300`); chip `WriteHandler.cpp:798-802`.
   Parity test with a synthetic timed attribute through the test seams
   (the `timedWritePaths` table is empty in the pin — inject one through
   a test-only setter in `schema` or the dispatcher's provider, say
   which): untimed concrete → 0xC6, timed → applied, wildcard untimed →
   skipped. `docs/matterjs-comparison.md:89` row and "Next" item 2
   (`:220-222`) updated; `BD-Matter-TimedAndQuotaDeferred`
   (`by_design.md:1453-1486`) retired per its own retire clause.
4. **A chip-tool group leg** in `TestChipToolSuite`
   (`internal/chiptool/suite_test.go:25-38`; state-changing legs last):
   with the interactive chip-tool (`interactive_test.go:132` `startInteractive`,
   `--advertise-operational` default true, `InteractiveCommands.h:33-46`):
   `groupsettings add-group` / `add-keysets` / `bind-keyset`
   (`examples/chip-tool/commands/group/Commands.h:88-96,193-215,268-309`),
   the device-side join with `groupcast join-group` (generated
   `zzz_generated/chip-tool/zap-generated/cluster/Commands.h:6494-6502`;
   ADR 0010's assumptions: UseAuxiliaryACL, FF05::FA, privacy on), then
   `onoff toggle` to `0xffffffffffff<groupId>` (`ModelCommand.cpp:29-37`
   `SendGroupCommand`) and a read of OnOff on the daemon's light. Skip
   with `requireIPv6` (`familyrun_test.go:825`) as the multicast cases do;
   CI's `chiptool` job already turns a whole-leg multicast skip into an
   error (`.github/workflows/chiptool.yml:268-275`). `docs/matterjs-comparison.md:62`
   row → ✅ naming this leg and the existing proofs (GC/2.8, ACE/1.6,
   SC/5.2 with chip's TH; G/3.2, SC/5.3 with the Go TH); "Next" item 1
   (`:214-219`) closed.
5. **A chip-tool subscription leg** in the same suite: the interactive
   chip-tool holds a subscription (`subscribe-by-id … keepSubscriptions
   auto-resubscribe`, `commands/clusters/ReportCommand.h:300-312`), the
   daemon restarts (`bridge_test.go:487` `restart`), the app pipe changes
   the contact sensor (`:580` `pipe`), and the change appears in
   chip-tool's output (`interactive.output()`) without a new
   SubscribeRequest in the daemon log (`msg=matter.rx.im.subscribe`, as
   `matterjs_test.go:221-259` checks for the matter.js controller) — the
   daemon re-established it as the initiator (`bridge/subscription_reestablish.go:95-`,
   ADR 0008). chip-tool's `ReadClient.cpp:606-620` accepts the unsolicited
   priming report on a matching subscription id. The negative leg
   (chip-tool itself restarted → InvalidSubscription, the subscription
   dropped) is optional; say whether it runs. `docs/matterjs-comparison.md:93`
   row's "not yet exercised" sentence goes; "Next" item 3 (`:223-226`)
   closed.
7. **Further device types and semantic tags on a device (owner decision
   2026-11-03, from 29b-2).** The device kit declares one device type per
   endpoint (`device.Device.Spec` device.go:692 `DeviceType: uint16(d.typ.ID)`;
   `endpoint.Spec.DeviceType` spec.go:52-54; `endpoint/helpers.go:17-28`
   `listedDeviceTypes` derives the Descriptor DeviceTypeList as primary +
   BridgedNode, a part its own alone), and no TagList declaration — the
   Descriptor server has the opt-in (`cluster/core/descriptor.go:254-259`
   `EnableTagList`, `:265-` `AddTags` after `DescriptorServer.ts:122-147`)
   but nothing on `Spec` reaches it. matter.js's bridge test instance
   declares both: the composed device's `deviceTypeList` [BridgedNode rev 3,
   PowerSource 0x0011 rev 1] (`BridgeTestInstance.ts:323-335`) and
   `CommonNumberTag` One/Two on its two sensors through
   `DescriptorServer.with("TagList")` + `descriptor.tagList` (`:306-317`);
   `DescriptorServer.ts:91-110` `addDeviceTypes` dedupes by id and keeps a
   listed type's revision. The daemon works around the first with a
   `Spec()` wrapper (`examples/reference-bridge/bridge_sim.go:101-108` at
   c44eb88e) and its unit test tolerates the second as the parts' one
   `Descriptor.TAGLIST` violation (`bridge_sim_test.go`). One shape, as
   API design (owner 2026-10-20): `device.Options` (options.go:25-52)
   gains `DeviceTypes []uint16` — further device types the endpoint lists
   after its own and before BridgedNode, deduped as `addDeviceTypes`,
   revision from `schema.DeviceTypeRevision` — and `Tags []SemanticTag`
   (`mattercore.SemanticTag`, the alias node/facade.go gives it in the
   M9 module wave) — the endpoint's Descriptor opts into TagList and serves
   them; nil leaves TagList off, which stays the default for the Apple
   reason in `descriptor.go:250-253`. They travel on `endpoint.Spec` as
   `DeviceTypes` and `Tags` (beside `DeviceConditions`, the same pattern
   as matter.js `Endpoint.Options.deviceConditions` → `Endpoint.DeviceConditions`
   types.go:186), `listedDeviceTypes` lists them, so `descriptorDeviceTypes`
   (materialize.go:570-577, used at `:272/:308/:337`) and the device-type
   validator (`endpoint/devicetype_conformance.go`, which reads the
   composition from the same list) follow without a second edit — Power
   Source 0x0011 on the composed device then mandates its PowerSource
   cluster through the validator, and the Duplicate-condition TAGLIST
   requirement on sibling parts (`:1383-1395` waives it only for an
   aggregator's bridged children) is met by the tags. Parts
   (`Spec.Parts`) take both as well. Then the daemon's wrapper goes and
   `bridge_sim.go` declares `DeviceTypes: {0x0011}` on the composed device
   and `Tags` One/Two on its parts; `TestBridgeSimFlag` tolerates no
   violation; TC-BR-4 stays 15/15 (the gocert run of §4). Parity tests:
   a `device` test after `BridgeTestInstance.ts:306-335` (list order,
   dedupe, revision, TagList FeatureMap bit only with tags) and the
   existing descriptor TagList tests. The M9 wave's read side
   (`node.BridgedDevice.Tags`, m9/module `node/lifecycle.go:129-134`) shows
   the declared tags unchanged. Docs: `docs/` device-kit page (declaration
   list), CHANGELOG Added, migration none (additive).
6. **Status and docs.** `TestCertifiabilityDocument` regenerated (0 class
   (a) rows; CNETWIFI and EEVSE summary rows move); `docs/roadmap.md:361`
   progress; `docs/certifiability.md` prose; `docs/matter-parity-contract.md`
   guard rows for the timed-write parity test and the two legs; CHANGELOG.

## Rules

Model: the Wi-Fi server and the generator change are Opus work; the
reviewer (Fable) re-reads `cluster/core/network_commissioning_wifi.go`
(fail-safe coupling) and every register entry. Author identity before
every commit (`SukramJ <sukramj@icloud.com>`), no trailers; one commit
per deliverable `(phase 30)`; guards under the flock with
`PATH=$HOME/go/bin:$PATH`, logs `~/.cache/go-fabric-scratch/p30/` (summary
log with HEAD before/after), grep not cat; the chip-tool harness alone
under the flock; read slices; cite at 862ad75f / 6170af84; `secure/`,
`commissioning/`, ACL and `cluster/core`'s four servers are not edited.
Guards: the WHOLE `make ci` to the end on the final tree (lint before
`cover-check`; the floor ratchet also bites > 3 points above), race on
`./cluster/core/... ./cluster/energy/... ./endpoint/... ./bridge/...
./internal/chiptool/...`, `go test ./script/clustergen/...`,
`make chiptool-test`, `GOFABRIC_CHIP_FAMILIES=CNETWIFI,EEVSE make
chiptool-families`, `TestCertifiabilityDocument`, `cite-check`,
`reachability-check`, `apidiff-check`, `ahead-check`, `mkdocs build
--strict`. Every new test file moves the citation and reachability
snapshots: run `make cite-check` and `make reachability` last and commit
what they regenerate. Rebase onto `origin/main` before the PR; PR without
auto-merge; report in the fixed shape (per §, guards with HEAD,
differences, decisions, left out, open questions, by_design candidates,
every citation listed).
