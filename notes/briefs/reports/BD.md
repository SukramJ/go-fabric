# Report BD — milestone 2, phase 14a: the pin lift completed, the citation guard

Branch `m2/phase-14-pin`, on top of the owner's d8fe15d (pin lift 9a6c2fb,
concept, briefs). I read matter.js only with `git show 862ad75f:…` and
`git ls-tree 862ad75f` on `../matter.js`. I did not read chip. Nothing
changed under `notes/parity/`, `docs/adr/`, `CLAUDE.md`,
`internal/chipdm/acknowledged.go`, `script/testparity/`,
`parity/schema.json` or in the generated schema files.

| Commit | Item |
| --- | --- |
| fe2024f `endpoint: a bridged node's PartsList composes its full family …` | 1 |
| ce4a3b1 `citations: the cite-check corrections at matter.js 862ad75f` | 3 |
| 090a577 `script/citecheck: the matter.js citation guard, make cite-check, the cite-check CI job` | 2, 4, 5 |
| `cite-check: build the tool instead of go run; report BD` | 2 (exit 2), report |

## Guards

| Guard | HEAD before → after | Result |
| --- | --- | --- |
| `make chiptool-test` (once, after item 1) | fe2024f → fe2024f | exit 0 (details in §1) |
| `make ci` | 090a577 → 090a577 | exit 0 (build, vet, test, lint 0 issues, mod tidy/verify, cover-check) |
| `make reachability-check` | 090a577 → 090a577 | exit 0. Regenerated in 090a577: one new test package, 5287 → 5295 entry points, unreached still **5**, so the ratchet did not move |
| `make chipdm-check` (`CHIP_ROOT=../connectedhomeip`) | 090a577 → 090a577 | exit 0 |
| `make race` | 090a577 → 090a577 | exit 0 |
| `make cite-check` (`MATTERJS_DIR=../matter.js`) | 090a577, and again after the Makefile fix | **exit 1: 18 hard errors**, all in the owner's list below. The committed artefacts reproduce exactly (no diff). With a missing clone: exit 2 and its own message |

`make cite-check` is **not green**, and neither will the new `cite-check`
CI job be, until the owner moves the 18 hard errors. The brief asks for both
"green at your HEAD" and "leave the citation for the owner where the name
stands several times or not at all". The second rule leaves hard errors that
I may not fix, so I cannot meet the first. This is question Q1.

## 1. BridgedNode composes its full family

**Built.** In `endpoint/assembler.go`, `buildEndpointTree` builds the parts
first. An endpoint whose listed device types compose full-family then gets
every descendant as its `PartIDs` (ascending); any other endpoint gets its
direct parts. Two helpers in `endpoint/helpers.go`:

- `listedDeviceTypes(ep)`: a part lists its own type; a bridged endpoint
  lists its primary type (when non-zero) and then BridgedNode.
- `composesFullFamily(ep)`: reads
  `schema.DeviceTypeDefinitionOf(id).Composition` at run time. There is no
  switch on device-type ids.

In `endpoint/materialize.go` both Descriptor DeviceTypeLists now come from
`listedDeviceTypes` (`descriptorDeviceTypes`). The list the Descriptor
serves and the list the composition is read from therefore cannot diverge.
The `PartIDs` doc in `endpoint/types.go` is updated.

matter.js at `862ad75f`:
- `packages/node/src/behaviors/descriptor/DescriptorServer.ts:227-254`,
  `#currentPartsListNumbers`:
  - `:232` takes the `IndexBehavior`'s `partsByNumber` when
    `this.#composesFullFamily` holds;
  - `:237-241` removes the endpoint itself;
  - `:242-247` lists the direct indexed parts otherwise;
  - `:253` sorts the result.
- `DescriptorServer.ts:262-270`, `#composesFullFamily`: true when some
  entry of the DeviceTypeList (or, without one, the endpoint type's device
  type) has `effectiveComposition === EndpointComposition.FullFamily`.
- `packages/model/src/standard/elements/bridged-node.element.ts:17`:
  `composition: "full-family"`. The brief names `BridgedNodeDT.ts`, which
  does not exist at the pin; this is the file. `aggregator.element.ts:17`
  and `root-node.element.ts:17` are full-family too.
- `packages/node/src/endpoints/bridged-node.ts:121`: `IndexBehavior` is
  mandatory on a BridgedNode endpoint. So the `agent.has(IndexBehavior)`
  check at `:232` holds and the full-family branch runs. `IndexBehavior.ts:121-141`
  (`#add`) indexes every descendant recursively.

**Root and aggregator: read, not touched.** Neither uses
`buildEndpointTree`, and both are already full-family, parts included,
recomputed on every read:
- `bridge/bridge.go:1505-1530`, `AttachRootPartsListProvider`, installs a
  live closure on the root Descriptor (0x001D) through the duck-typed
  `SetPartsListProvider`.
- `node/root.go:457-473`, `attachPartsListProviders`, wires two closures:
  - the root's lists every endpoint except the root itself;
  - the aggregator's lists every endpoint except the root, the aggregator
    and the root's direct children (the OTA provider; see
    `node/ota_provider.go:112`, `isRootChild`).

**Guard.** `endpoint/parts_test.go`, `TestBridgedNodePartsListIsFullFamily`:
a bridged Refrigerator with two TemperatureControlledCabinet parts, one of
which has a PowerSource part. The fridge's PartsList holds all three
descendants, the intermediate cabinet's holds only its child, and the leaves
hold none. The existing one-level `TestSpecPartsComposeAChildEndpoint` is
unchanged and green.

`make chiptool-test` on fe2024f: exit 0.
- Passed: `TestChipToolCommissionsReferenceBridge` (PASE, AddNOC, CASE),
  `TestChipToolSuite`, `TestGoControllerCommissionsReferenceBridge` and
  `…SubscribesReferenceBridge`; 338 subtests PASS in all.
- Skipped: `TestMatterJSController`. It looks for a built matter.js at the
  worktree's `../matter.js` (`.claude/worktrees/matter.js`), which does not
  exist, and the shared checkout's working tree is off limits for this
  brief.
- The reference bridge has no nested parts, so its PartsLists do not change.

**Decisions.** The composition is read from the listed device types, as
matter.js reads the DeviceTypeList. **Left out.** Nothing. **Questions.**
None.

## 2. `script/citecheck`

**Built.** `main.go` (package doc = the spec for the reader), `collect.go`,
`check.go`, `fixes.go`, `citecheck_test.go` and `testdata/`. The committed
artefacts are `drift.json` and `summary.md`. Its shape follows
`script/reachability`. All git access is plumbing on `MATTERJS_DIR`:
`rev-parse`, `ls-tree`, and one `cat-file --batch` process. Exit codes:
0 = clean, 1 = hard errors (artefacts still written), 2 = no clone or no pin.
`-report` turns exit 1 into 0, and `-fixes <file>` writes the derived
corrections.

**Guard.** `TestCitecheckDriftOnRatchet` holds `citecheckDriftRatchet = 266`
exactly. The ratchet comment says how the number was measured.
`TestCitecheckSummaryAgreesWithDrift` checks that the counts agree and that
the measurement was taken at the pin. The collector, resolver, checker and
derived fixes are tested against `testdata/sample.go.txt`, an in-memory tree
and `testdata/expect.json`; the expected citation strings live in JSON so
the guard does not read them as the module's own. Coverage is 69.1 %; I
added a floor of 68 in `script/coverfloor/floors.go`.

**Decisions** (where the spec left room, or where following it literally
measured nonsense):
1. **Tree roots `examples/ packages/ support/`**, not `packages` alone.
   The Go code cites `support/codegen/…`, `support/chip-testing/…` and
   `examples/device-bridge-onoff/…`. With `packages` only, 50 correct
   citations were "file not found". A path from any of the three roots is
   resolved exactly. (Q2)
2. **Which names belong to a citation.** In a sentence with several
   references, each name goes to the nearest one. A bare `:NN` and another
   language's citation (`foo.go:12`) absorb names but are not checked
   themselves: bare line parts often point at a file named in an earlier
   sentence, and checking them gave 41 false hard errors. Names that are
   the stem of a file the sentence cites do not count.
3. **Loose and strict matching.** For the drift test, a name starting
   upper-case also matches as the tail of a longer name (`ReceiveAccept`
   in `BdxReceiveAccept`). A correction only moves to an exact occurrence.
4. **The sentence** ends at `.`, `!` or `?` before white space, except after
   e.g/i.e/cf/vs/etc. On a code line, the sentence is the string literal
   that contains the citation. A path broken across lines after `/`, or
   between `.ts` and `:NN`, is rejoined.
5. **`at <sha>`**: of a sentence's shas, the one nearest the citation that
   is a matter.js commit wins. One sentence also names chip's `6170af84`.
6. **Exit 2 survives `make`.** `make cite-check` builds the tool instead of
   using `go run`, because `go run` turns every non-zero exit into 1.

## 3. The corrections

All corrections were applied by script, with every anchor asserted on its Go
line. They change comments only.
- **19 file names lengthened.** 13 picked by a name of the sentence near the
  cited line in exactly one candidate. 6 picked by the added rule "the one
  candidate the same Go file cites by a resolving path"; this rule resolved
  the remaining `BleScanner.ts`, `Endpoint.ts` and `on-off-light.ts`
  citations.
- **19 lines moved.** 18 drifts and 1 line beyond the end of its file.

**Deviation from the brief (Q3).** The brief says to move every drift whose
name stands exactly once in the file. A reviewed sample of 13 such moves
found about half wrong: the nearest name belonged to another clause. Two
examples:
- `WindowCoveringServer.ts:189` would have gone to a `liftPositionAware`
  line;
- `LevelControlServer.ts:624` would have gone to a `targetValue` line.

I therefore moved a citation only when its sentence ties the name to it by
parentheses: in the citation's own group, as in `Foo.bar (Foo.ts:12)`, or as
in `Foo.ts:12 (`bar`)`. I reviewed all 23 attached moves line by line and
vetoed 5 as wrong; those keep their original line. Unattached
single-occurrence drifts are in the owner's list with their candidate line.

## 4. CI job `cite-check`

`.github/workflows/ci.yml`, after `reachability`:
- reads the pin from `parity/schema.json`;
- clones with `git clone --filter=blob:none --no-checkout`, then
  `sparse-checkout set examples packages support`, then checks out the pin;
- runs `make cite-check MATTERJS_DIR=…`.

The comment says why the job is on the merge path despite needing the
network (fixed commit, deterministic content; the same stance as `chipdm`)
and that a failed clone or exit 2 is red. Until the owner's corrections land
the job **will be red** (Q1). It is not part of `make ci`.

## 5. Docs

- `docs/matter-parity-contract.md` §4: a new row "matter.js citations" (what
  it checks, what fails it, how to regenerate). The link is an absolute
  GitHub URL.
- The package doc of `script/citecheck` says the same for the reader of the
  code.
- `CHANGELOG.md` `[Unreleased]`: one entry under Added (the guard) and one
  under Changed (item 1).

## The citation numbers (pin `862ad75f`)

| | before corrections | after |
| --- | --- | --- |
| Citations / Go files | 3 716 / 643 | 3 716 / 643 |
| Resolved: exact (root path) | 781 | 781 |
| Resolved: unique suffix | 113 | 132 |
| Resolved: unique basename | 2 790 | 2 790 |
| Checked at their own `at <sha>` | 249 | 249 |
| Drift-checked / hard only | 1 750 / 1 928 | 1 762 / 1 936 |
| **Hard errors** | **38** (28 ambiguous, 5 not found, 5 beyond end) | **18** (9 ambiguous, 4 not found, 5 beyond end) |
| **Drift** | **284** | **266** |

**`citecheckDriftRatchet` = 266.** The concept counted 3 696 citations;
mine is higher because of the brief commits since then and because broken
paths are now rejoined.

## Questions

- **Q1.** `cite-check` (local and CI) stays red until the owner moves the 18
  hard errors below. Should it land red, or should I add a committed
  acknowledged list of hard errors? I added neither without your
  decision.
- **Q2.** Do you accept the tree roots `examples/ packages/ support/` in
  place of the brief's `packages`?
- **Q3.** Do you accept that a drift moves only when its name is tied to the
  citation by parentheses (the five vetoes are listed below)? If you want
  the literal rule, `-fixes` together with a scratch apply script does it
  in one run.
- **Q4.** `internal/chipdm/acknowledged.go:68` (`ValueValidator.ts:172-178`,
  ambiguous) is one of the 18, and it is in a file I may not touch.

## by_design candidates

None. A citation line is not a divergence.

## Owner's citations

### Hard errors left (18)

- `binding/reason.go:37` `BindingManager.ts:721-737`: line beyond end of file; name `ignoredEntryReason`; candidates: 252, 707
- `bridge/commissioning_window.go:545` `AdministratorCommissioningServer.ts:283-290`: line beyond end of file; name `—`; candidates: —
- `cluster/energy/meter.go:42` `packages/node/src/behaviors/meter-identification/MeterIdentificationServer.ts:15`: line beyond end of file; name `—`; candidates: —
- `cluster/matter_negative_write_parity_test.go:341` `packages/node/src/standard/elements/window-covering-cluster.element.ts:72`: file not found; name `—`; candidates: —
- `cluster/matter_negative_write_parity_test.go:358` `packages/node/src/standard/elements/window-covering-cluster.element.ts:79`: file not found; name `—`; candidates: —
- `cluster/matter_negative_write_parity_test.go:673` `packages/node/src/standard/elements/window-covering-cluster.element.ts:72`: file not found; name `—`; candidates: —
- `controller/ble/scanner.go:99` `BleScanner.ts:124-674` at `0ceb202d`: ambiguous; name `—`; candidates: packages/nodejs-ble/src/BleScanner.ts, packages/protocol/src/common/BleScanner.ts, packages/react-native/src/ble/BleScanner.ts
- `controller/ble/scanner_parity_matterjs_test.go:614` `BleScanner.ts:476-496`: ambiguous; name `—`; candidates: packages/nodejs-ble/src/BleScanner.ts, packages/protocol/src/common/BleScanner.ts, packages/react-native/src/ble/BleScanner.ts
- `controller/client/endpoint.go:22` `endpoints/root.ts:314-319`: ambiguous; name `—`; candidates: packages/main/src/forwards/endpoints/root.ts, packages/node/src/endpoints/root.ts
- `controller/client/network.go:266` `ClientEventEmitter.ts:156-175`: line beyond end of file; name `—`; candidates: —
- `controller/client/node.go:148` `node/client/ClientNodePhysicalProperties.ts:19-134`: line beyond end of file; name `—`; candidates: —
- `controller/exchange/errors.go:32` `errors.ts:17` at `0ceb202d`: ambiguous; name `—`; candidates: packages/cli-tool/src/errors.ts, packages/general/src/transaction/errors.ts, packages/model/src/common/errors.ts, packages/model/src/decoration/errors.ts, packages/node/src/endpoint/errors.ts, packages/protocol/src/action/errors.ts, packages/protocol/src/groups/errors.ts, packages/protocol/src/protocol/errors.ts, packages/protocol/src/thread/errors.ts, packages/testing/src/docker/errors.ts, packages/thread-br-client/src/diagnostic/errors.ts, support/codegen/src/chipdm/errors.ts
- `device/controller_types_test.go:17` `on-off-light-switch.ts:75-95` at `0ceb202d`: ambiguous; name `—`; candidates: packages/main/src/forwards/devices/on-off-light-switch.ts, packages/node/src/devices/on-off-light-switch.ts
- `internal/chipdm/acknowledged.go:68` `ValueValidator.ts:172-178`: ambiguous; name `—`; candidates: packages/model/src/logic/definition-validation/ValueValidator.ts, packages/node/src/behavior/state/validation/ValueValidator.ts
- `node/node_test.go:286` `Endpoint.ts:985`: ambiguous; name `—`; candidates: packages/matter.js/src/device/Endpoint.ts, packages/node/src/endpoint/Endpoint.ts
- `secure/mattercert/validate_rcac_test.go:424` `packages/.../Rcac.ts:140`: file not found; name `—`; candidates: —
- `secure/setup/decode.go:132` `Schema.ts:18-25`: ambiguous; name `—`; candidates: packages/model/src/models/Schema.ts, packages/types/src/schema/Schema.ts
- `secure/setup/decode.go:378` `Schema.ts:18-25`: ambiguous; name `—`; candidates: packages/model/src/models/Schema.ts, packages/types/src/schema/Schema.ts

### Vetoed moves (5)

- `bridge/fabric_session_close_test.go:44` `ExchangeManager.ts:716-726` — rule would move to `ExchangeManager.ts:693-703` (drift: gracefulClose stands once, at 693); reviewed: the original line is right
- `cluster/core/basic_information_test.go:672` `basic-information.element.ts:165-169` — rule would move to `basic-information.element.ts:88-92` (drift: CapabilityMinima stands once, at 88); reviewed: the original line is right
- `cluster/core/group_key_management_test.go:346` `TlvObject.ts:306-310` — rule would move to `TlvObject.ts:124-128` (drift: fabricIndex stands once, at 124); reviewed: the original line is right
- `controller/handshake/server_test.go:211` `CaseServer.ts:309` — rule would move to `CaseServer.ts:93` (drift: InvalidParam stands once, at 93); reviewed: the original line is right
- `controller/interaction/report.go:75` `ClientSubscriptionHandler.ts:89` — rule would move to `ClientSubscriptionHandler.ts:112` (drift: reportStarted stands once, at 112); reviewed: the original line is right

### Drift left (261; candidate lines = the name's exact occurrences in the cited file)

- `bdx/messages.go:287` `BdxAcceptMessagesSchema.ts:66-80` — `SendAccept`: 49
- `bdx/outbound.go:58` `OutboundFlow.ts:93-110` — `readDataChunk`: 70
- `bdx/responder.go:174` `BdxProtocol.ts:204-211` — `SessionFor`: no exact occurrence; nearest loose match 84
- `bdx/responder.go:294` `BdxProtocol.ts:159-164` — `onNewExchange`: 110
- `bdx/send.go:39` `bdx-session-initiator.ts:155-165` — `MaxBlockSize`: no exact occurrence; nearest loose match 200
- `bdx/send_serve_test.go:79` `bdx-session-initiator.ts:126-153` — `SendInit`: 57
- `bdx/session.go:139` `bdx-session-initiator.ts:155-178` — `buildInitMessage`: 37, 48, 114
- `binding/manager.go:388` `BindingManager.ts:147-157` — `sourceEndpoint`: 119, 120, 253, 297, 580
- `binding/manager.go:470` `BindingManager.ts:513-520` — `#scheduleGroupRecheck`: 482, 497
- `binding/manager.go:82` `BindingManager.ts:333` — `forAddress`: 317, 445
- `binding/resolve.go:304` `BindingManager.ts:585-627` — `#establishClientKind`: 328, 569
- `binding/resolve.go:344` `BindingManager.ts:629-643` — `#clearPending`: 137, 594, 613
- `binding/resolve.go:65` `BindingManager.ts:255-353` — `#resolveAndEmit`: 128, 235, 239, 418, 521
- `ble/bluez/central_linux.go:410` `NobleBleChannel.ts:299-440` — `openChannel`: 103, 178
- `bridge/ackpump_test.go:524` `MessageExchange.ts:428-433` — `requiresAck`: 464, 466, 471, 487, 537, 676, … (10 in all)
- `bridge/bridge.go:1280` `Fabric.ts:408-415` — `initiateForceClose`: 437, 486
- `bridge/bridge.go:1585` `Session.ts:248` — `initiateClose`: 292, 320
- `bridge/bridge.go:617` `ExchangeManager.ts:425-431` — `CloseSession`: no exact occurrence; nearest loose match 693
- `bridge/fabric_session_close_test.go:170` `Fabric.ts:408-415` — `UpdateNOC`: 574
- `bridge/fabric_session_close_test.go:43` `Fabric.ts:408-415` — `initiateClose`: 434, 451
- `bridge/fields_reader.go:138` `packages/protocol/src/action/server/CommandInvokeResponse.ts:446-448` — `TlvSchema`: 22, 549
- `bridge/fields_reader.go:172` `LevelControlServer.ts:596` — `#optionsAllowExecution`: 270, 355, 422, 483, 729
- `bridge/fields_reader_range_test.go:22` `packages/protocol/src/action/server/CommandInvokeResponse.ts:446-448` — `requestTlv`: 483, 484, 486
- `bridge/fields_reader_test.go:927` `LevelControlServer.ts:596` — `ExecuteIfOff`: 494
- `bridge/handlers.go:355` `PaseServer.ts:155-157` — `SessionParameters`: no exact occurrence; nearest loose match 194
- `bridge/handlers.go:950` `MRP.ts:129` — `retransmissionIntervalOf`: 147, 162
- `bridge/im_gate.go:32` `packages/protocol/src/interaction/InteractionMessenger.ts:240` — `ReadRequest`: 74, 85, 282, 353, 356, 1139, … (7 in all)
- `bridge/invoke_batch_dispatch_test.go:13` `InteractionServer.ts:1043-1074` — `suppressedBuffer`: 1165, 1176, 1178, 1182, 1185, 1193
- `bridge/invoke_batch_dispatch_test.go:151` `InteractionServer.ts:950-955` — `invokeRequests`: 976, 985, 1017, 1019, 1048, 1089
- `bridge/outbound_reliable.go:24` `InteractionMessenger.ts:721` — `DataReport`: 86, 165, 171, 284, 368, 467, … (12 in all)
- `bridge/outbound_reliable_test.go:201` `MRP.ts:129` — `retransmissionIntervalOf`: 147, 162
- `bridge/receive.go:197` `MessageExchange.ts:428-433` — `requiresAck`: 464, 466, 471, 487, 537, 676, … (10 in all)
- `bridge/receive.go:242` `MessageCodec.ts:377` — `protocolId`: 50, 93, 102, 159, 161, 162, … (11 in all)
- `bridge/receive_dispatch.go:253` `packages/protocol/src/interaction/InteractionMessenger.ts:679` — `suppressResponse`: 153, 157, 333, 336, 484, 488, … (25 in all)
- `bridge/receive_dispatch.go:301` `InteractionMessenger.ts:719-721` — `sendDataReportMessage`: 510, 795, 799
- `bridge/receive_dispatch.go:412` `MessageExchange.ts:602` — `requiresAck`: 464, 466, 471, 487, 537, 676, … (10 in all)
- `bridge/receive_dispatch.go:454` `CommandInvokeResponse.ts:292-295` — `InvokeResponse`: no exact occurrence; nearest loose match 403
- `bridge/receive_dispatch.go:531` `packages/node/src/node/server/InteractionServer.ts:1043-1074` — `suppressedBuffer`: 1165, 1176, 1178, 1182, 1185, 1193
- `bridge/receive_test.go:218` `MessageCodec.ts:377` — `vendorId`: 457, 458, 516, 521, 526, 527
- `bridge/receive_test.go:478` `InteractionServer.ts:409-412` — `TimedRequest`: 50, 1217
- `bridge/reply_doorlock_event_test.go:95` `DoorLockServer.ts:1017-1022` — `FabricIndex`: 26, 935, 1326, 1456
- `bridge/securechannel.go:1141` `ExchangeManager.ts:663-671` — `#sendCloseSession`: 693, 716
- `bridge/securechannel.go:1180` `ExchangeManager.ts:663-671` — `#sendCloseSession`: 693, 716
- `bridge/securechannel_close_parity_test.go:168` `ExchangeManager.ts:716-726` — `requiresAck`: 413, 441
- `bridge/status_response_decode_test.go:116` `InteractionMessenger.ts:721` — `waitForSuccess`: 221, 844, 953
- `bridge/subscribe.go:1073` `ServerSubscription.ts:610-614` — `InvalidAction`: 710
- `bridge/subscribe.go:1158` `ThermostatServer.ts:451` — `reactTo`: 168, 480, 501, 552, 568, 609, … (13 in all)
- `bridge/subscribe.go:1245` `packages/node/src/behavior/state/managed/Datasource.ts:776-796` — `onChange`: 270, 367, 403, 431, 482, 737, … (9 in all)
- `bridge/subscribe.go:607` `ServerSubscription.ts:866-876` — `InvalidSubscription`: 939
- `bridge/subscribe.go:718` `packages/protocol/src/interaction/InteractionMessenger.ts:347` — `sendDataReport`: 369, 470
- `bridge/subscribe.go:798` `packages/node/src/node/server/ServerSubscription.ts:764` — `#sendUpdateMessage`: 555, 893
- `bridge/subscribe_dispatch.go:461` `InteractionMessenger.ts:721` — `waitForSuccess`: 221, 844, 953
- `bridge/subscribe_encryption_test.go:27` `ServerSubscription.ts:764` — `#sendUpdateMessage`: 555, 893
- `bridge/subscribe_establish_test.go:35` `InteractionMessenger.ts:719-721` — `waitForSuccess`: 221, 844, 953
- `bridge/subscribe_report_chunking_test.go:243` `packages/protocol/src/interaction/InteractionMessenger.ts:347` — `sendDataReport`: 369, 470
- `bridge/subscribe_report_routing_test.go:73` `packages/node/src/node/server/ServerSubscription.ts:823` — `initiateExchange`: 124, 900
- `bridge/timed_conformance_test.go:82` `CommandInvokeResponse.ts:291` — `StatusResponse`: 20, 560
- `bridge/write_chunked_dispatch_test.go:203` `InteractionServer.ts:521-532` — `WriteResponse`: 52, 563, 564, 573
- `cluster/closure/closurecontrol_payload_shapes_test.go:142` `closure-control.element.ts:80-81` — `MotionLatching`: 26
- `cluster/core/access_control.go:1076` `packages/types/src/datatype/CaseAuthenticatedTag.ts:31-33` — `getVersion`: 76, 88
- `cluster/core/access_control.go:197` `packages/model/src/standard/elements/access-control.element.ts:35-39` — `ChangeTypeEnum`: 82, 97, 136
- `cluster/core/basic_information_parity_test.go:534` `basic-information.element.ts:51` — `SerialNumber`: 75
- `cluster/core/binding.go:96` `packages/node/src/behaviors/binding/BindingManager.ts:714-718` — `entryKey`: 122, 132, 381, 530, 608, 618, … (8 in all)
- `cluster/core/binding_change_test.go:57` `BindingManager.ts:714-718` — `entryKey`: 122, 132, 381, 530, 608, 618, … (8 in all)
- `cluster/core/descriptor.go:158` `DescriptorServer.ts:241-250` — `#serverList`: 41, 177, 275
- `cluster/core/descriptor.go:298` `packages/node/src/behaviors/descriptor/DescriptorServer.ts:241-250` — `#serverList`: 41, 177, 275
- `cluster/core/descriptor_test.go:258` `DescriptorServer.ts:241-250` — `#serverList`: 41, 177, 275
- `cluster/core/general_commissioning.go:944` `packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:67-82` — `CommissioningComplete`: 247
- `cluster/core/general_diagnostics_test.go:184` `GeneralDiagnosticsServer.ts:99` — `#validateTestEnabledKey`: 145, 159, 191
- `cluster/core/group_key_management_failure_test.go:121` `packages/model/src/standard/elements/group-key-management.element.ts:58` — `GroupKeyMap`: 29
- `cluster/core/groupcast.go:554` `GroupcastServer.ts:268-277` — `GroupKeyMap`: 41, 517, 518, 758, 780
- `cluster/core/groupcast.go:667` `GroupcastServer.ts:269` — `retainedSenderOnly`: 289, 314, 622, 632, 661, 901
- `cluster/core/groupcast_parity_matterjs_test.go:244` `GroupcastServer.ts:206-209` — `groupProperties`: 90, 107, 354, 606, 615, 620, … (16 in all)
- `cluster/core/operational_credentials.go:1315` `OperationalCredentialsServer.ts:110` — `AttestationNonce`: 54, 64
- `cluster/core/operational_credentials.go:1859` `Fabric.ts:557-559` — `PublicKeyError`: 50, 615
- `cluster/core/operational_credentials.go:1899` `Fabric.ts:557-559` — `PublicKeyError`: 50, 615
- `cluster/core/operational_credentials.go:2023` `Fabric.ts:548` — `nodeId`: 55, 65, 103, 168, 378, 383, … (24 in all)
- `cluster/core/operational_credentials.go:2204` `DeviceAdvertiser.ts:84-86` — `cancelAll`: 111, 318
- `cluster/core/operational_credentials.go:241` `Fabric.ts:548` — `UpdateNOC`: 574
- `cluster/core/operational_credentials.go:337` `OperationalCredentialsServer.ts:247-252` — `addNoc`: 208, 272, 308, 382
- `cluster/core/operational_credentials_nocvalidation_test.go:236` `Fabric.ts:548` — `nodeId`: 55, 65, 103, 168, 378, 383, … (24 in all)
- `cluster/core/operational_credentials_nocvalidation_test.go:28` `Fabric.ts:557-559` — `PublicKeyError`: 50, 615
- `cluster/core/ota_provider.go:584` `SoftwareUpdateManager.ts:1219-1232` — `NotifyUpdateApplied`: 1429
- `cluster/core/scenes_attributes.go:287` `OnOffServer.ts:97-100` — `OnOff`: 14, 17, 22, 43, 45, 198
- `cluster/core/scenes_management.go:362` `ScenesManagementServer.ts:77-80` — `OnOff`: 552, 563
- `cluster/core/scenes_management.go:373` `OnOffServer.ts:114-117` — `OnOff`: 14, 17, 22, 43, 45, 198
- `cluster/core/scenes_onoff_test.go:26` `OnOffServer.ts:159-165` — `ScenesManagement`: 267
- `cluster/core/scenes_onoff_test.go:31` `OnOffServer.ts:119-122` — `OnOff`: 14, 17, 22, 43, 45, 198
- `cluster/cover/windowcovering_parity_matterjs_test.go:361` `window-covering-cluster.element.ts:72` — `GoToLiftPercentage`: 92
- `cluster/cover/windowcovering_payload_shapes_test.go:114` `window-covering-cluster.element.ts:71` — `LiftPercent100thsValue`: 95
- `cluster/cover/windowcovering_payload_shapes_test.go:199` `window-covering-cluster.element.ts:71` — `CurrentPositionLiftPercentage`: 47
- `cluster/cover/windowcovering_server.go:208` `window-covering-cluster.element.ts:130-131` — `OperationalStatus`: 55
- `cluster/cover/windowcovering_server.go:707` `window-covering-cluster.element.ts:128-131` — `OperationalStatus`: 55
- `cluster/levelcontrol/levelcontrol_server.go:37` `LevelControlServer.ts:114-120` — `LevelControl`: 30, 51, 58, 62, 268, 290, … (34 in all)
- `cluster/levelcontrol/levelcontrol_server_test.go:243` `LevelControlServer.ts:114-120` — `LevelControl`: 30, 51, 58, 62, 268, 290, … (34 in all)
- `cluster/levelcontrol/native.go:272` `LevelControlServer.ts:186-195` — `MoveMode`: 389, 395, 845
- `cluster/levelcontrol/native.go:472` `LevelControlServer.ts:624-627` — `targetValue`: 543
- `cluster/levelcontrol/native.go:64` `LevelControlServer.ts:529-550` — `#turnOnForLevel`: 590, 597
- `cluster/levelcontrol/native_parity_matterjs_test.go:757` `LevelControlServer.ts:624-627` — `preCommit`: 694
- `cluster/light/colorcontrol_set.go:30` `packages/node/src/behavior/Transitions.ts:455-534` — `RemainingTime`: 749
- `cluster/light/colorcontrol_set_parity_matterjs_test.go:182` `Transitions.ts:455-534` — `RemainingTime`: 749
- `cluster/lock/doorlock_server.go:124` `DoorLockServer.ts:69` — `alwaysSet`: 119, 120, 122
- `cluster/matter_negative_write_parity_test.go:314` `packages/node/src/behaviors/thermostat/ThermostatServer.ts:594-601` — `occupiedHeatingSetpoint`: 334, 341, 762, 1051
- `cluster/matter_negative_write_parity_test.go:328` `packages/node/src/behaviors/thermostat/ThermostatServer.ts:614` — `systemMode`: 781, 1237
- `cluster/matter_negative_write_parity_test.go:646` `thermostat-cluster.element.ts:511` — `SetpointRaiseLowerModeEnum`: 343, 531
- `cluster/media/playback.go:69` `media-playback.element.ts:96-98` — `MediaPlayback`: 19, 20, 225
- `cluster/onoff/server.go:613` `OnOffServer.ts:97-100` — `OnOff`: 14, 17, 22, 43, 45, 198
- `cluster/onoff/server.go:81` `OnOffServer.ts:97-100` — `OnOff`: 14, 17, 22, 43, 45, 198
- `cluster/onoff/server_internal_test.go:75` `OnOffServer.ts:62` — `OnOff`: 14, 17, 22, 43, 45, 198
- `cluster/spec/conformance.go:103` `packages/model/src/aspects/Conformance.ts:995-997` — `computeApplicability`: 152, 913
- `cluster/spec/instance.go:483` `conformance-compiler.ts:857-858` — `addEnumMemberValidation`: 217, 763
- `cluster/thermo/thermostat_parity_test.go:198` `packages/model/src/standard/elements/thermostat-cluster.element.ts:510-514` — `SetpointRaiseLowerModeEnum`: 343, 531
- `cluster/thermo/thermostat_parity_test.go:276` `thermostat-cluster.element.ts:510-514` — `SetpointRaiseLowerModeEnum`: 343, 531
- `cluster/thermo/thermostat_payload_shapes_test.go:127` `thermostat-cluster.element.ts:509-513` — `SetpointRaiseLowerModeEnum`: 343, 531
- `cluster/thermo/thermostat_payload_shapes_test.go:29` `thermostat-cluster.element.ts:322-323` — `SetpointRaiseLower`: 340
- `cluster/thermo/thermostat_payload_shapes_test.go:374` `thermostat-cluster.element.ts:517-521` — `ControlSequenceOfOperationEnum`: 110, 538
- `cluster/thermo/thermostat_server.go:136` `packages/node/src/behaviors/thermostat/ThermostatServer.ts:594-601` — `systemMode`: 781, 1237
- `cluster/thermo/thermostat_server.go:173` `ThermostatServer.ts:219-220` — `SetpointRaiseLower`: 185
- `cluster/thermo/thermostat_server.go:578` `ThermostatServer.ts:614` at `0ceb202d` — `systemMode`: 781, 1237
- `cluster/thermo/thermostat_server.go:789` `thermostat-cluster.element.ts:510-514` — `SetpointRaiseLowerModeEnum`: 343, 531
- `cluster/thermo/thermostat_server.go:914` `ThermostatServer.ts:219-220` at `0ceb202d` — `SetpointRaiseLower`: 185
- `cluster/thermo/thermostat_server.go:957` `thermostat-cluster.element.ts:322-323` — `SetpointRaiseLower`: 340
- `cluster/wire/fancontrol.go:11` `fan-control.element.ts:69-74` — `FanControl`: 18, 19, 121
- `cluster/wire/modebase.go:23` `mode-base.element.ts:44-56` — `ModeBase`: 18, 19, 98
- `cluster/wire/operationalstate.go:22` `operational-state.element.ts:57-80` — `OperationalState`: 19, 20, 40, 120
- `contract/matter.go:645` `packages/node/src/behaviors/thermostat/ThermostatServer.ts:451` — `reactTo`: 168, 480, 501, 552, 568, 609, … (13 in all)
- `controller/attestation/cert.go:95` `Certificate.ts:587-596` — `parseDate`: 559, 677, 678
- `controller/ca/ca.go:580` `kinds/definitions/operational.ts:109-110` at `0ceb202d` — `keyUsage`: 44
- `controller/client/addresses.go:24` `Peers.ts:959-966` — `expirationOf`: 704, 714, 726, 729, 998
- `controller/client/addresses_internal_test.go:45` `Peers.ts:615-634` — `#cullExpiredNodesAndAddresses`: 669, 679
- `controller/client/cache.go:86` `DatasourceCache.ts:199-201` — `invalidateVersion`: 314
- `controller/client/cache_internal_test.go:160` `DatasourceCache.ts:161-163` — `externalSet`: 26, 139, 292, 311, 349, 380
- `controller/client/cluster.go:101` `ClientStructure.ts:762-808` — `#synchronizeCluster`: 240, 547, 701, 872, 1316
- `controller/client/cluster.go:53` `ClientStructure.ts:1613-1629` — `ClusterStructure`: 66, 423, 523, 660, 686, 713, … (20 in all)
- `controller/client/config.go:36` `NetworkClient.ts:432-439` — `AutoSubscribe`: no exact occurrence; nearest loose match 130
- `controller/client/config.go:41` `NetworkClient.ts:412-421` — `DefaultSubscription`: no exact occurrence; nearest loose match 468
- `controller/client/endpoint.go:45` `ClientStructure.ts:1020-1033` — `#endpointFor`: 221, 268, 529, 639, 687, 1096, … (8 in all)
- `controller/client/endpoint.go:57` `ClientStructure.ts:858-905` — `DeviceType`: 995, 1641
- `controller/client/endpoint.go:73` `packages/node/src/node/client/ClientStructure.ts:871-897` — `#node`: 16, 17, 126, 167, 175, 202, … (17 in all)
- `controller/client/fabricops.go:124` `CommissioningClient.ts:775-779` — `DiscoveryData`: 66, 591, 1280
- `controller/client/fabricops.go:183` `CommissioningClient.ts:883-895` — `NetworkAddress`: 915, 934, 978
- `controller/client/fabricops.go:203` `CommissioningClient.ts:664-680` — `#catsChanged`: 183, 696
- `controller/client/fabricops.go:373` `CommissioningClient.ts:406-408` — `PeerInitiatedCloseError`: 76, 440
- `controller/client/label.go:27` `NetworkClient.ts:259-307` — `#reconcileFabricLabel`: 60, 426
- `controller/client/network.go:185` `NetworkClient.ts:215-238` — `#recomputeConnectionState`: 54, 55, 56, 57, 87, 107, … (9 in all)
- `controller/client/node.go:254` `NetworkClient.ts:60-102` — `ClientNode`: 12, 481
- `controller/client/node.go:44` `NetworkClient.ts:385-391` — `isNewlyCommissioned`: 146, 147, 568
- `controller/client/node.go:52` `NetworkClient.ts:268-273` — `fabricLabelReconciled`: 427, 431, 584
- `controller/client/node_test.go:66` `NetworkClient.ts:331-372` — `autoSubscribe`: 43, 124, 130, 143, 516, 556, … (9 in all)
- `controller/client/reactions.go:144` `Peers.ts:837-858` — `#onShutdown`: 755, 876
- `controller/client/reactions_test.go:65` `Peers.ts:812-824` — `startUp`: 756, 904
- `controller/client/record.go:130` `CommissioningClient.ts:836-845` — `AdvertisedIntervals`: 868, 1102, 1103
- `controller/client/record.go:462` `behavior/system/commissioning/RemoteDescriptor.ts:214-234` — `toLongForm`: 167
- `controller/client/record.go:489` `RemoteDescriptor.ts:91-143` — `fromLongForm`: 52
- `controller/client/record.go:657` `CommissioningClient.ts:586` — `peerAddress`: 173, 175, 207, 325, 375, 377, … (18 in all)
- `controller/client/registry_test.go:347` `CommissioningClient.ts:353-357` — `OperationalCredentials`: 101, 383, 398, 432
- `controller/client/structure.go:181` `ClientStructure.ts:1674-1692` — `AttributeList`: 33, 106, 755, 787, 845, 885, … (9 in all)
- `controller/client/structure.go:195` `ClientStructure.ts:1601-1605` — `AttributeUpdates`: 415, 571, 573, 672, 686, 1814
- `controller/dcl/cert.go:31` `DclCertificateService.ts:2089` — `CertificateKind`: 49, 483, 499, 826, 1164, 1179, … (9 in all)
- `controller/dcl/crl.go:55` `DclCertificateService.ts:235-241` — `parseCrl`: 1592, 1803, 2020
- `controller/dcl/github.go:29` `packages/general/src/util/Github.ts:131-133` — `GitHub`: 21, 24, 28, 37
- `controller/discovery/parity_matterjs_test.go:47` `DnssdName.ts:25` — `installedAt`: 87, 140, 151, 215, 394, 411
- `controller/exchange/exchange.go:52` `MessageExchange.ts:1272-1273` — `suppressPeerLoss`: 141, 142, 157, 158, 299, 313, … (10 in all)
- `controller/exchange/exchange_test.go:244` `MessageExchange.ts:894-918` — `PeerUnresponsiveError`: 12, 781
- `controller/exchange/manager.go:178` `ExchangeManager.ts:391-440` — `addProtocolHandler`: 126
- `controller/exchange/manager.go:185` `SecureChannelProtocol.ts:146-148` — `CaseServer`: 23, 92, 99
- `controller/handshake/case.go:61` `PeerConnection.ts:829-860` — `CaseClient`: 11, 555
- `controller/handshake/pase_test.go:101` `PaseClient.ts:192-197` — `InvalidParam`: 95, 104, 108, 111
- `controller/interaction/handler.go:49` `InteractionMessenger.ts:938-972` — `readDataReports`: 1012
- `controller/interaction/interaction.go:140` `ClientInteraction.ts:1249-1251` — `maximumPeerResponseTime`: 1108, 1333, 1334
- `controller/interaction/interaction.go:165` `InteractionMessenger.ts:148-161` — `StatusResponse`: 73, 208, 226, 265, 991, 1444
- `controller/interaction/interaction.go:43` `InteractionMessenger.ts:186-190` — `MatterFlowError`: 20, 806, 998, 1156, 1352, 1389, … (7 in all)
- `controller/interaction/interaction.go:53` `ControllerCommissioningFlow.ts:519-523` — `StatusResponseError`: 50, 438, 460, 1981
- `controller/interaction/interaction.go:55` `InteractionMessenger.ts:201-214` — `ReceivedStatusResponseError`: 29, 268, 993
- `controller/interaction/invoke.go:66` `InteractionMessenger.ts:1082-1105` — `StatusResponse`: 73, 208, 226, 265, 991, 1444
- `controller/interaction/parity_matterjs_test.go:53` `ClientSubscription.ts:104` — `SubscriptionId`: 9, 34, 55
- `controller/interaction/provider.go:177` `ClientInteraction.ts:1112` — `verifyReachability`: 1159
- `controller/interaction/read.go:111` `InteractionMessenger.ts:1006-1009` — `dataVersionFilters`: 1149, 1153, 1163, 1174, 1187, 1194
- `controller/interaction/read.go:116` `InteractionMessenger.ts:998-1050` — `#shortenDataVersionFilters`: 1163, 1170
- `controller/interaction/read.go:29` `InteractionMessenger.ts:998-1050` — `DataVersionFilters`: 1191
- `controller/interaction/report.go:449` `InteractionMessenger.ts:914-931` — `DataReport`: 86, 165, 171, 284, 368, 467, … (12 in all)
- `controller/interaction/report_internal_test.go:159` `InteractionMessenger.ts:998-1050` — `DataVersionFilters`: 1191
- `controller/interaction/scripted_test.go:175` `ClientInteraction.ts:438-440` — `TimedRequest`: no exact occurrence; nearest loose match 422
- `controller/interaction/scripted_test.go:296` `InteractionMessenger.ts:938-972` — `TlvReadRequest`: 44, 85, 360, 1140
- `controller/interaction/scripted_test.go:85` `InteractionMessenger.ts:1082-1105` — `StatusResponse`: 73, 208, 226, 265, 991, 1444
- `controller/interaction/scripted_test.go:86` `ClientInteraction.ts:457-464` — `commandRef`: 97, 510, 513, 516, 535, 538, … (34 in all)
- `controller/interaction/subscribe.go:112` `InteractionMessenger.ts:938-972` — `SubscribeResponse`: 76, 88, 1108
- `controller/interaction/subscribe.go:224` `InteractionMessenger.ts:1052-1055` — `#encodeReadingRequest`: 1140, 1143, 1198
- `controller/interaction/subscribe_test.go:65` `InteractionMessenger.ts:938-972` — `SubscribeResponse`: 76, 88, 1108
- `controller/interaction/write.go:25` `Write.ts:204-218` — `WriteRequest`: 9, 19, 29
- `controller/interaction/write.go:48` `ClientInteraction.ts:369-371` — `TimedRequest`: no exact occurrence; nearest loose match 422
- `controller/interaction/write_test.go:114` `Write.ts:74` — `AccessControl`: 10, 14, 15, 16
- `controller/interaction/write_test.go:167` `InteractionMessenger.ts:1138-1162` — `StatusResponse`: 73, 208, 226, 265, 991, 1444
- `controller/peer/connection.go:52` `types/src/protocol/definitions/general.ts:33` — `GeneralStatusCode`: 7
- `controller/peer/connection.go:786` `PeerConnection.ts:674-696` — `handleConnectionError`: 490, 656
- `controller/peer/observer.go:26` `Peer.ts:190-195` at `0ceb202d` — `#save`: 105, 575
- `controller/peer/probe.go:36` `interaction/InteractionMessenger.ts:938-972` — `StatusResponse`: 73, 208, 226, 265, 991, 1444
- `device/device.go:529` `packages/node/src/endpoints/bridged-node.ts:39-73` — `BridgedNode`: 135
- `device/type.go:169` `packages/node/src/node/client/ClientStructure.ts:871` — `#node`: 16, 17, 126, 167, 175, 202, … (17 in all)
- `device/type.go:62` `RequirementGenerator.ts:188` — `mandatory`: 50, 52, 53, 208, 218, 228, … (9 in all)
- `endpoint/dispatcher.go:272` `AttributeWriteResponse.ts:329-331` — `UnsupportedWrite`: 233
- `endpoint/materialize.go:532` `OnOffServer.ts:97-122` — `OnOff`: 14, 17, 22, 43, 45, 198
- `endpoint/types.go:657` `OnOffServer.ts:97-100` — `OnOff`: 14, 17, 22, 43, 45, 198
- `examples/reference-bridge/ota.go:54` `OtaRequestorTestInstance.ts:166-168` — `CanConsent`: no exact occurrence; nearest loose match 338
- `examples/reference-controller/cli/self.go:223` `packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:243-250` at `0ceb202d` — `ApplyUpdate`: 405
- `examples/reference-controller/cli/window.go:140` `CommissioningClient.ts:538-549` — `#revokeStaleCommissioningWindow`: 492, 518, 570
- `examples/reference-controller/cli/window.go:170` `CommissioningClient.ts:788` — `EnhancedCommissioningWindow`: 509, 820
- `examples/reference-controller/cli/window.go:203` `CommissioningClient.ts:454-465` — `openBasicCommissioningWindow`: 486, 494
- `examples/reference-controller/cli/window.go:31` `packages/node/src/behavior/system/commissioning/CommissioningClient.ts:454` — `commissioningTimeout`: 486, 495, 508, 520, 532, 565, … (7 in all)
- `examples/reference-controller/cli/window.go:35` `CommissioningClient.ts:492` — `randomBytes`: 524
- `groups/request_chunk_test.go:30` `Write.ts:149-171` at `0ceb202d` — `AccessControl`: 10, 14, 15, 16
- `im/client.go:104` `interaction/InteractionMessenger.ts:1036-1037` — `TlvDataVersionFilter`: 38, 1171, 1174, 1181
- `im/client.go:146` `interaction/InteractionMessenger.ts:1124-1136` — `sendTimedRequest`: 1440
- `im/invoke.go:152` `packages/protocol/src/action/server/CommandInvokeResponse.ts:446-448` — `requestTlv`: 483, 484, 486
- `im/invoke.go:387` `packages/node/src/node/server/InteractionServer.ts:1043-1074` — `suppressedBuffer`: 1165, 1176, 1178, 1182, 1185, 1193
- `im/invoke.go:407` `packages/node/src/node/server/InteractionServer.ts:950-955` — `invokeRequests`: 976, 985, 1017, 1019, 1048, 1089
- `im/invoke_batch_test.go:117` `InteractionServer.ts:950-955` — `invokeRequests`: 976, 985, 1017, 1019, 1048, 1089
- `im/invoke_batch_test.go:135` `InteractionServer.ts:1043-1074` — `SuppressResponse`: 412, 596, 1162, 1192
- `im/read.go:442` `packages/protocol/src/action/server/AttributeReadResponse.ts:238-274` — `addConcrete`: 92, 202
- `im/subscription/handler_parity_test.go:294` `packages/node/src/node/server/ServerSubscription.ts:301-311` — `#addOutstandingAttributes`: 346, 380, 574
- `im/subscription/handler_parity_test.go:343` `packages/node/src/node/server/ServerSubscription.ts:314-337` — `#handleClusterStateChanges`: 359, 735
- `im/subscription/handler_parity_test.go:352` `packages/node/src/node/server/ServerSubscription.ts:320-322` — `#isClosed`: 135, 365, 386, 418, 455, 559, … (13 in all)
- `im/subscription/handler_parity_test.go:416` `packages/node/src/node/server/ServerSubscription.ts:666-668` — `#isClosed`: 135, 365, 386, 418, 455, 559, … (13 in all)
- `im/subscription/handler_parity_test.go:447` `packages/node/src/node/server/InteractionServer.ts:549-566` — `keepSubscriptions`: 616, 624, 647, 662
- `im/subscription/subscription.go:245` `packages/node/src/node/server/ServerSubscription.ts:412` — `#sendUpdatesActivated`: 152, 429, 460, 819
- `im/write.go:41` `InteractionServer.ts:521-532` — `WriteResponse`: 52, 563, 564, 573
- `im/write_parity_test.go:355` `packages/protocol/src/action/server/AttributeWriteResponse.ts:32` — `WriteResult`: 10, 55, 58, 80, 138, 168, … (10 in all)
- `node/client.go:32` `BindingManager.ts:333` — `PeerSet`: 16, 584, 588
- `node/config.go:142` `NetworkServer.ts:202-222` — `network`: 21, 239
- `node/config.go:31` `packages/node/src/node/ServerNode.ts:107-127` — `commissioning`: 8
- `node/config.go:35` `NetworkServer.ts:202-222` — `network`: 21, 239
- `node/observers.go:46` `FabricManager.ts:253` — `deleted`: 49, 159, 233, 272
- `node/ota.go:55` `OtaRequestorTestInstance.ts:166-168` — `CanConsent`: no exact occurrence; nearest loose match 338
- `node/security.go:550` `CaseServer.ts:210` — `SessionManager`: 12, 53, 56
- `schema/fabric_scoped.go:21` `operational-credentials.element.ts:104` — `SetVidVerificationStatement`: 142
- `schema/fabric_scoped.go:31` `scenes-management.element.ts:37-156` — `ScenesManagement`: 18, 19, 226
- `secure/channel/session.go:313` `Session.ts:248` — `initiateClose`: 292, 320
- `secure/mattercert/validate_rcac_test.go:112` `Certificate.ts:101` — `signEcdsa`: 139, 275
- `secure/mattercert/validate_rcac_test.go:427` `Certificate.ts:101` — `signEcdsa`: 139, 275
- `secure/operational/manager.go:104` `NodeSession.ts:368` — `gracefulClose`: 403
- `secure/operational/manager.go:313` `SessionManager.ts:484` — `getPaseSession`: 570
- `secure/operational/manager.go:50` `packages/protocol/src/session/NodeSession.ts:368` — `gracefulClose`: 403
- `secure/operational/manager.go:789` `packages/protocol/src/session/SessionManager.ts:490-508` — `getNextAvailableSessionId`: 537
- `secure/operational/manager.go:815` `packages/protocol/src/session/SessionManager.ts:513` — `getSession`: 542, 564
- `secure/operational/manager.go:918` `Session.ts:248` — `initiateClose`: 292, 320
- `secure/operational/manager_close_parity_test.go:11` `packages/protocol/src/session/NodeSession.ts:368` — `gracefulClose`: 403
- `secure/operational/manager_close_parity_test.go:9` `packages/protocol/src/session/Session.ts:248` — `initiateClose`: 292, 320
- `secure/operational/manager_session_id_lifecycle_test.go:37` `packages/protocol/src/session/SessionManager.ts:490-508` — `getNextAvailableSessionId`: 537
- `secure/operational/manager_session_seed_test.go:10` `SessionManager.ts:213` — `randomUint16`: 264
- `secure/sigma/protocol.go:292` `MRP.ts:129` — `retransmissionIntervalOf`: 147, 162
- `secure/sigma/sigma.go:250` `MRP.ts:129` — `retransmissionIntervalOf`: 147, 162
- `secure/sigma/sigma1_session_params_test.go:13` `MRP.ts:129` — `retransmissionIntervalOf`: 147, 162
- `secure/spake2/session_params_test.go:16` `PaseServer.ts:151` — `SessionParameters`: no exact occurrence; nearest loose match 194
- `store/fabric.go:222` `Fabric.ts:548` — `nodeId`: 55, 65, 103, 168, 378, 383, … (24 in all)
- `tlv/codec_parity_test.go:245` `packages/types/test/tlv/TlvStringTest.ts:86` — `TlvByteString`: 9, 102, 105, 113, 119, 126, … (8 in all)
- `transport/message/fuzz_message_test.go:114` `MessageCodec.ts:481-504` — `encodePacket`: 440
- `transport/message/message.go:93` `packages/protocol/src/protocol/ExchangeManager.ts:196-197` — `applicationPayload`: 228
- `transport/mrp/ack.go:219` `MessageExchange.ts:428-433` — `requiresAck`: 464, 466, 471, 487, 537, 676, … (10 in all)
- `transport/mrp/ack_test.go:233` `MessageExchange.ts:428-433` — `requiresAck`: 464, 466, 471, 487, 537, 676, … (10 in all)
- `transport/mrp/counter.go:17` `NodeSession.ts:115` — `initiateClose`: 349, 350, 389
- `transport/tcp/frame_test.go:116` `TcpChannel.ts:211-218` — `maxPayloadSize`: 40, 41, 47, 72, 75, 76, … (7 in all)
