# Report AC — matter.js pin lift to `0ceb202d`: enum ranges, cross-check, families doc, citations, register

Branch `wave11/schema` from `planning/briefs-wave-11` (`e576eea`, which
carries the regenerated snapshot `7677c1f`), pushed, no PR. matter.js read
at `0ceb202d` (`../matter.js`), the old pin via
`git -C ../matter.js show 4633bfd7:<path>`; chip at the harness pin
`6170af84`. One commit per item; `make reachability` after each (the
inventory moved only with §1: unreached unchanged at 5, entry points
4497 → 4500).

| Commit | Item |
| --- | --- |
| `feat(spec): enum range members reach the runtime` | §1 |
| `chore(chipdm): classify the enum ranges of the 0ceb202d snapshot` | §2 |
| `docs(certifiability): correct the families this module does not run yet` | §3 |
| `docs: renumber matter.js citations to the 0ceb202d pin` | §4 |
| `docs: pin references and CHANGELOG for the 0ceb202d snapshot` | §5 |
| `notes: report AC` | §6 + this report |

Guards on the branch head: `make ci` exit 0 (171 test packages ok, 0 FAIL;
golangci-lint 0 issues; coverfloor 173 packages at or above their floor),
`make race` exit 0 (171 packages ok, 0 FAIL, no DATA RACE),
`make chipdm-check CHIP_ROOT=…/connectedhomeip` PASS
(TestChipDataModelCrosscheck, TestChipDataModelPins,
TestCompareSeededDifferences), `make reachability-check` exit 0 (unreached
5). The chip-tool jobs were not run (the harness belongs to brief AD).

## §1 Enum manufacturer ranges reach the runtime

- `cluster/spec/model.go`: `spec.EnumRange{Name; Min, Max uint64;
  Conformance}` with `Contains`, and `Enum.Ranges []EnumRange`. Cited to
  `packages/model/src/models/FieldModel.ts:isEnumRange` (a field without an
  id whose constraint has min and max, under an enum).
- `cluster/spec/instance.go` `EnumSupported`: a named value first, then the
  range that holds the value; that member's conformance decides. Cited to
  `ValueValidator.ts:172-178` (lines 172 `ranges = members.filter(… isEnumRange)`
  and 178 `!valid.has(value) && !ranges.some(range => range.constraint.test(value))`)
  and `conformance-compiler.ts:857-858` (`memberValidators?.[value] ??
  rangeValidators.find(({ range }) => range.test(value))?.validate`). A range
  member with no stated conformance is optional (no enforcement), as the
  conformance compiler treats it.
- `script/clustergen/gen.go`: `enumRanges` emits every id-less member with a
  `constraint.min`/`max` as `Ranges: []spec.EnumRange{…}` (name kept). No
  constants are generated for ranges.
- Regenerated `cluster/spec/` and the golden `rvcrunmode.go.golden`
  (`go test ./script/clustergen -update`). The diff adds ranges to exactly:
  ModeChangeStatus MfgCodes and ModeTag MfgTags in the ten mode clusters,
  ErrorStateEnum ManufacturerError and OperationalStateEnum
  ManufacturerStates in OperationalState, RvcOperationalState,
  OvenCavityOperationalState, ClosureControl ClosureErrorEnum
  ManufacturerError — **and `IlluminanceMeasurement.LightSensorTypeEnum`
  `Ms` 64–254**, which the brief did not list. It is in the snapshot
  (`illuminance-measurement.element.ts:45` `Field({ name: "Ms", conformance:
  "O", constraint: "64 to 254" })`, resource text "Reserved for manufacturer
  specific light sensor types") and matter.js validates it the same way, so
  it is generated like the others (open question 1).

Parity cases. None of the affected enums backs a writable attribute (every
attribute typed by them is `R`, ModeTag sits in the read-only
SupportedModes struct), so the cases go against `spec` directly:

- `cluster/matter_negative_write_parity_test.go`
  `TestEnumManufacturerRangeParity`: for each shape (8-bit 128..191 in
  ModeChangeStatus, OperationalStateEnum, ErrorStateEnum (two clusters),
  ClosureErrorEnum; 16-bit 32768..49151 in ModeTag of two clusters)
  `spec.EnumSupported` accepts both ends and refuses the value just past
  (192, 49152) and just below (127, 32767); `Instance.CheckValue` (the value
  checks of `ValidateWrite` after access) accepts OperationalState 191 and
  answers 192 with CONSTRAINT_ERROR; `ValidateWrite` of the read-only
  attribute stays UNSUPPORTED_WRITE.
- `cluster/spec/instance_test.go` `TestEnumSupportedRanges`: range
  conformance (an `X` range refuses), a named value wins over a range that
  holds it, `Contains` bounds.

Host setters:

| Setter | Before | Now |
| --- | --- | --- |
| `opstate` `validState` / `validError` (OperationalStateList entries, `SetOperationalError`, `ReportError` event codes) | already accepted 0x80–0xBF through the extra `clusterwire.HasOperationalStateLabel` clause | **changed**: the clause is gone, `spec.EnumSupported` on the generated definition decides (same set: 128–191 accepted, 192 refused). `TestParityMatterJS_OperationalStateManufacturerRanges` pins 0x80/0xBF accepted, 0x7F/0xC0 refused for states and errors |
| `modebase` SupportedModes tags | no value check on a tag at all — any value, manufacturer range included, was and is accepted | unchanged |
| `modebase` `validHostStatus` (ChangeToMode status from the host) | accepts `st >= 0x80` | unchanged — wider than the range (192–255 accepted); open question 2 |
| `closure` `ReportError` (CurrentErrorList / OperationalError) | no value check | unchanged |

LightSensorType: there is no setter. `cluster/measurement`'s Illuminance
instance enables only `Tolerance` (`definitions.go:39`), the server does not
serve LightSensorType, and nothing under `cluster/measurement` names it.
The generated definition types the attribute `LightSensorTypeEnum` with
the `Ms` range, so a future setter that validates through the definition
gets 0, 1 and 64–254. Nothing changed.

Guards: `go test ./cluster/... ./script/...`, `make ci`.

## §2 CHIP data-model cross-check

- `internal/chipdm/acknowledged.go`: the stale
  `IlluminanceMeasurement.LightSensorType type` acknowledgement removed.
- The 18 differences classified `ClassMatterJS` in a new section "(i)
  matter.js models the specification's enum ranges", one reason constant
  (`enumRange`, the brief's text) and five path patterns (the file supports
  `*.` prefixes): `*.ModeTag.MfgTags` (10), `*.ErrorStateEnum.ManufacturerError`
  (3), `*.OperationalStateEnum.ManufacturerStates` (3),
  `ClosureControl.ClosureErrorEnum.ManufacturerError` (1),
  `IlluminanceMeasurement.LightSensorTypeEnum.Ms` (1), sources
  `matter.js packages/model/src/standard/elements/{mode-base,operational-state,closure-control,illuminance-measurement}.element.ts`.
  The 18 the check reported are these, not the brief's list:
  ModeChangeStatus MfgCodes produces no difference (the enum is already
  acknowledged as absent from chip, `*.ModeChangeStatus datatype`), and the
  18th is LightSensorTypeEnum Ms. `compare.go` untouched.
- `docs/chip-datamodel-crosscheck.md` regenerated: snapshot row now
  `0ceb202d…`, class (i) 96 → 113, the LightSensorType row gone, five range
  rows added.

Guard: `make chipdm-check CHIP_ROOT=…` PASS (TestChipDataModelCrosscheck,
TestChipDataModelPins, TestCompareSeededDifferences).

## §3 `docs/certifiability.md` §"Families this module does not run yet"

Rewritten against `chipFamilies` (99 families) and the code:

- The label and localisation families (FLABEL, ULABEL, LCFG, LTIME, LUNIT)
  run; they are no longer in the table.
- **(c) not yet run**: PWRTL, ILL, PRS, DISHM — the module serves them
  (`measurement.NewPowerTopologyServer`, `NewIlluminanceServer`,
  `NewPressureServer`, `modebase.NewDishwasherMode`) but the reference
  daemon exposes no endpoint with them and the harness runs no family; EEM,
  EPM — served (`NewElectricalEnergyServer`, `NewElectricalPowerServer`)
  and carried by the daemon's ElectricalMeter
  (`examples/reference-bridge/fleet_energy_market.go:503-542`), no family
  run. These are the matter.js families (`support/chip-testing/test/
  app-fast`, `core`) plus the chip families of servers this module ships
  (`TC_EEM_*`, `TC_EPM_*`, `TC_PWRTL_*`, `TC_DISHM_*` Python,
  `Test_TC_ILL_*`, `Test_TC_PRS_*` YAML at `6170af84`) that `chipFamilies`
  lacks. The brief's "(c)" is written "(c) not yet run" as the brief says;
  the document's class legend defines (c) as "harness" (open question 3).
- **(b)**: media (APBSC, LOWPOWER, WAKEONLAN named — the media families
  matter.js runs) until concept v2 phase 9; ICDB. **(d)** rows unchanged.
- BDX paragraph: `BD-chip-DiagLogs-NoBDX` still exists
  (`notes/parity/by_design.md:1328`) and DLOG is 1 case, class (c) manual
  in the families table. The paragraph now says that BDX shipped with the
  OTA requestor in `v0.3.0` (ADR 0019) as the receiving side, that DLOG
  needs the sending side, and that a RetrieveLogsRequest asking for BDX with
  a `TransferFileDesignator` is still answered inline
  (`cluster/core/diagnostic_logs.go` never reads `RequestedProtocol`), until
  concept v2 phase 8. The SU requestor no longer appears as waiting for BDX.

Guard: `go test ./internal/chiptool/` (TestCertifiabilityDocument,
TestWorkflowRunsEveryFamily, TestFamilyDeviceTypesResolve) PASS.

## §4 Citation renumbering

Applied in one simultaneous pass (the table has chains such as
`FailsafeContext.ts:154 → 153` while `:153 → 152`, and
`MessageExchange.ts:660 → 665` while `:665` stays), keyed by basename and
range, over every `.go` and `.md` file except `notes/briefs/**` (reports
included, as historical), `notes/parity/sweeps/**` and the two owner-only
files: 330 citations in 107 files, then compound and bare follow-on numbers
by hand. Every changed Go line is a comment; in
`cluster/core/operational_credentials.go`, `cluster/lock/`,
`cluster/thermo/` only comment lines changed, `endpoint/materialize.go`
untouched. `CHANGELOG.md` released entries were renumbered too (the brief
excludes only `notes/briefs` and sweeps). ADR 0018's "Measured against
matter.js `4633bfd7`" and `notes/handover.md:127` ("re-read at the local
checkout `4633bfd7`") record past measurements and keep the old pin.

**Rows already off at `4633bfd7`.** The drift table matches text at the old
pin; several citations did not point at the construct their comment names
even there (they were written against an older or the newer tree). The
brief's rule for non-moved rows — find the construct the comment describes
— was applied to these as well, including some rows the table marks
`moved`, where a mechanical renumbering would have pointed at unrelated
code:

| Citation | Table | Now | Comment describes |
| --- | --- | --- | --- |
| `ExchangeManager.ts:635` | moved → 640 (`result.push`) | `:693` | `observers.on(session.gracefulClose, () => this.#sendCloseSession(session))` |
| `ExchangeManager.ts:658` | CHANGED (brief: `:661-664`) | `:716-726` | `#sendCloseSession` — every one of the 11 uses says so (open question 4) |
| `ExchangeManager.ts:287` | ambiguous | `:340` | `exchange.session.id !== session.id` |
| `ExchangeManager.ts:400-405` | moved → 405-410 | `:425-431` | "the new message wins" channel update |
| `ExchangeManager.ts:220-224` | moved-partial | `:230-235` | unicast message with the privacy flag dropped |
| `ExchangeManager.ts:411-418` | ambiguous | `:441-452` | unsolicited message on an unknown exchange: standalone ack, close, drop |
| `Fabric.ts:508-538` | moved → 513-543 | `:541-579` | `FabricBuilder.setOperationalCert` |
| `Fabric.ts:524-526` | moved → 529-531 | `:557-559` | `PublicKeyError("Operational certificate does not match public key")` |
| `DoorLockServer.ts:69` | ambiguous | `:119` | supportedOperatingModes default `{vacation, privacy, passage, alwaysSet: 2047}` |
| `DoorLockServer.ts:119-127` | moved → 169-177 | `:193-196` | `lockDoor` |
| `DoorLockServer.ts:119-143` | moved-partial | `:193-214` | the four lock command handlers |
| `DoorLockServer.ts:140-142` | moved → 190-192 | `:209-214` | `unboltDoor` (AD) |
| `ThermostatServer.ts:169` | moved → 174 | `:200` | `amount *= 10` |
| `ThermostatServer.ts:186-196` | moved → 187-197 | `:189-198` | Heat/Cool without the feature → InvalidCommand |
| `ThermostatServer.ts:158-166` | moved-partial | `:189-193` | Heat without HEAT → InvalidCommand |
| `OperationalCredentialsServer.ts:453` | moved → 466 | `:449` | `failsafeContext.fabricIndex !== undefined` in addTrustedRootCertificate |
| `OperationalCredentialsServer.ts:451`, `:451-453` | ambiguous | `:442`, `:442-447` | `if (failsafeContext.rootCertSet)` |
| `OperationalCredentialsServer.ts:218` | ambiguous | `:224` | `const failsafeContext = this.#failsafeContext` in addNoc |
| `NodeSession.ts:187` | ambiguous | `:203` | decode nonce `generateNonce(…, this.#peerNodeId)` |

**Rows that already matched `0ceb202d` and keep their numbers**:
`Session.ts:355-356` (`get usesMrp()`), `ExchangeManager.ts:425-431`,
`MessageExchange.ts:665` (the protocolId line, `bdx/errors.go`),
`OtaSoftwareUpdateRequestorServer.ts:485-1285`, `:553-565`
(`#connectOtaProviderFor`), `:676-680` (Busy re-query at
`max(delayedActionTime, minimumQueryInterval)`), and `:1354` with its bare
`:1362` in `store/ota.go` — `activeOtaProviders` / `updateInProgressDetails`,
both "This value is persisted", which is what the comment says. The brief's
resolution for `:1354` (`updateQueryInterval = Hours(24)`, now `:1365`)
describes a different line than the comment (open question 4).
`CaseServer.ts:171-186`, `:302-327` and `OnOffServer.ts:267-289` keep their
numbers (only a field rename / a timer helper inside).

Other non-mechanical resolutions: `DescriptorServer.ts:236-244 → :241-250`,
`ManufacturerExtensibleIdentifier.ts:23-38 → :28-51`,
`ClusterId.ts:22-32 → :22-41`, `MessageExchange.ts:626-633 → :626-638`,
`:630-633 → :630-638`, `Fabric.ts:405-407 → :410-412`,
`OperationalCredentialsServer.ts:244-250 → :254-260`, `:457-459 → :470-472`,
`:458-459 → :471-474`, `Parts.ts:36-61 → :37-73`, `Parts.ts:170-201 → :195-213`
(the brief says `:195-211`; the function closes at 213), `ServerNode.ts:264-298
→ :274-309`, `:106-126 → :107-127`, `SessionParameters.ts:50-54 → :49-56`,
`:50-62 → :49-63`, `:55-62 → :58-63`, `:59-62 → :60-63`,
`ThermostatServer.ts:170-189 → :202-220`, `:218-219 → :219-220`,
`:732 → :727`, `:879 → :890` (`#assertSetpointWithinLimits`, which the
comment names), `DoorLockServer.ts:889 → :1188`, `:911-939 → :1289-1301`.
Comments naming the pin (`at 4633bfd7`, `(4633bfd7)`, `HEAD 4633bfd7`) now
say `0ceb202d`, with their bare follow-on numbers renumbered
(`Datasource.ts :956-957 → :1042-1043`, `ThermostatServer.ts :221, :223,
:246, :268 → :222, :224, :247, :269`, `:599-606 → :594-601`, `:619 → :614`);
WindowCoveringServer.ts, window-covering-cluster.element.ts and the device
files are unchanged between the pins, so only the label moved there.

Left as they are: `PasePairingTest.ts:108-159` (`secure/pase_server_parity_test.go:136`)
cites a case "do special successful pase process" that exists at neither
pin — a test file, left per the brief; `DoorLockServer.ts:1188 / :941` in
`cluster/lock/doorlock_server.go:354` — the bare `:941` (old: the schedule
`Restricted` throw) has no single counterpart in the rewritten file; AD owns
DoorLock.

### Cited behaviour changed

Not covered by brief AD:

- `MessageExchange.ts:626-633` / `:630-633` → `:626-638` / `:630-638`
  (`bridge/peer_loss_test.go:251`, `:480`): `#reportsPeerLoss` also returns
  false when the *session* suppresses peer loss
  (`!this.session.suppressPeerLoss`), not only the exchange or the
  operation.
- `Parts.ts:36-61` → `:37-73` (`device/device.go:515`): `add()` now asserts
  the part is not an ancestor of the owner (`assertNotAncestor`) and refuses
  endpoint number 0 ("belongs to the node root") before the insertion check.
- `Parts.ts:170-201` → `:195-213` (`device/device.go:517`): the number check
  is `IndexBehavior.assertNumberAvailable(endpoint, number, this.owner)` —
  against the endpoint's own node (a32933ac) — and every numbered endpoint
  of the inserted tree enters `usedNumbers`; before, only endpoints with
  parts did, so two leaves with the same number in one inserted tree went
  undetected.
- `DoorLockServer.ts:119-127` / `:119-143` → `:193-196` / `:193-214`
  (`cluster/lock/doorlock_server.go:267`, `doorlock_server_events_test.go:71`):
  the handlers no longer end in `#emitLockOperation`; they await
  `#actuate` (`:1015-1043`), which refuses a request without a fabric
  (UNSUPPORTED_ACCESS), runs `#authorize` (operating mode, lockout, PIN),
  calls the overridable `handleLockOperation` hook (a throw → LockOperationError
  + FAILURE), then sets lockState and emits through `#completeOperation`.
  DoorLock is AD's; listed because these two citations are not among AD's.
- `ManufacturerExtensibleIdentifier.ts:23-38` → `:28-51`
  (`cluster/core/access_control.go:1092`): `fromMei` also rejects a
  non-integer or out-of-32-bit MEI; the Go input is a `uint32`, so nothing
  differs for the bridge.

Covered by AD (renumbered only): `DoorLockServer.ts:140-142` → `:209-214`
(UnboltDoor now reports **Unlock** and sets Unlocked, the comment says
Unlatch), `:889` → `:1188` (wrong-code path is `#refuseWrongCode`
`:1175-1189` with `lockoutEndsAt`), `:911-939` → `:1289-1301`
(`#emitLockOperation(type, requester)`; a request without a fabric never
gets here, so "PASE → null" no longer arises); `DescriptorServer.ts:236-244`
→ `:241-250` (direct descendants filtered by `IndexBehavior.isIndexed`);
`ThermostatServer.ts:732` → `:727` (`#assertUserSetpointLimits` takes the
scope "Heat"/"Cool"), `:879` → `:890` (`#assertSetpointWithinLimits` body
rewritten), `:170-189` → `:202-220`.

### Renumbering the owner-only files need

Not applied (`by_design.md`, `matter_behaviour_findings.md` are owner-only);
the same mapping gives:

- `matter_behaviour_findings.md`: `Datasource.ts:349 → 410` (×2), `:949 →
  1035`, `OperationalCredentialsServer.ts:353 → 366`, `:112 → 113`,
  `Fabric.ts:508-538 → 541-579`, `Fabric.ts:524 → 557-558` (H1, H2: the
  PublicKeyError check; the brief's ":349 region" is `Fabric.matches`, a
  different comparison), `:403-410 → 408-415`, `FailsafeContext.ts:168-171 →
  167-170`, `:257 → 246`, `:153 → 152`, `:154 → 153` (and bare `:291 → 280`),
  `ExchangeManager.ts:287 → 340` (×2), `NodeSession.ts:111 → 115` (×2),
  `:324-327 → 349-352`, `DoorLockServer.ts:119-143 → 193-214`, `:822 →
  1289-1301` (bare `:911-939 → 1289-1301`), `ThermostatServer.ts:228-266 →
  229-267`, `OtaSoftwareUpdateRequestorServer.ts:620-627 → 633-640`.
- `by_design.md`: `ExchangeManager.ts:658-666 → 663-671`, `:669-676 → 674-681`
  (×2; bare `:700-706` → `:705-711`), `Parts.ts:170-201 → 195-213`,
  `conformance-compiler.ts:250-254 → 252-256`, `NodeSession.ts:312-322 →
  337-347`, `Fabric.ts:456-460 → 461-465`, `:405-407 → 410-412`,
  `OperationalCredentialsServer.ts:84-88 → 85-89`, `:486-505 → 504-523`
  (`setVidVerificationStatement`; it calls `fabric.updateVendorVerificationData`,
  the entry says `fabric.update`), `CommissioningServer.ts:157-171 → 143-157`,
  `ThermostatServer.ts:599-606 → 594-601`.

Guard: `go build ./...`, `make ci` (comment-only change), the parse/apply
script output (330 replacements, every changed Go line a comment).

## §5 Pin references and CHANGELOG

- `docs/concept-v2-scope-extension.md`: the "Achtung" sentences replaced by
  "Seit Welle 11 ist `0ceb202d` der Pin, den `parity/schema.json` und jede
  Zeilenangabe im Repo tragen."
- `script/matterjs-pin-distance.sh` reads the pin off `parity/schema.json`
  and names none: unchanged.
- CHANGELOG `[Unreleased]` → Changed: the snapshot move `85cf6647` (code pin
  `4633bfd7`) → `0ceb202d`, no revision moved; the manufacturer ranges
  accepted on write and by the host setters; LightSensorType an enum.

## §6 Findings register, Tier 1 and Tier 2 — the bullets without FIXED

| Bullet | Status | Proof |
| --- | --- | --- |
| H7 (IM) ongoing reports drop FabricFiltered/subject/ACL | fixed (C2's status block says FIXED; H7 is "the same defect") | `bridge/subscribe.go:157-170` (subTarget carries fabricIndex, subject, CATs, fabricFiltered, pase), `:424-428` (ongoing report ctx stamped), `:435` (`readAuthorizedResults`, ACL + read privilege, `:377-398`), `:269` (`authorizedEventReports`) |
| TLV/codec: vendor-qualified protocol IDs | fixed (batch 2 says so) | `bridge/receive.go:245` `if proto.HasVendorID && proto.VendorID != 0` → ErrUnknownProtocol before the switch |
| TLV/codec: security flags (mask, unknown types, Control, echo) | fixed in effect | `transport/message/message.go:45` 2-bit mask, `:281-286` rejects session type ∉ {0,1} and the C bit; a unicast P frame is dropped (`bridge/privacy.go:69-71`). `bridge/reply.go:183-184` still copies `Privacy`/`Control` from the request header — dead, since neither can be set on an accepted unicast request; the raw byte is kept for the nonce (`message.go:224-239`) |
| TLV/codec: EndOfContainer tag bytes | fixed | `tlv/decode.go:65` `readTag(kind)` before the `TypeEndContainer` return (`:70-81`), and a non-anonymous tag on it is an error |
| IM: empty keepalive SuppressResponse | fixed (batch 2 says so) | `bridge/subscribe.go:451-453` |
| Core: SetRegulatoryConfig CountryCode | open — deferred by batch 1 with reason (Apple RemoveFabric after a non-empty value) | `bridge/fields_reader.go:466-479` still reads `el.Octets` (a UTF-8 string lands in `el.String`, so CountryCode is always ""); `cluster/core/general_commissioning.go:688-692` only checks the length; Location is never written from it |
| CASE: unresolvable destinationId | fixed (batch 3 had kept it as deliberate; it changed since) | `secure/sigma/protocol.go:806-811` returns `ErrNoSharedTrustRoots` on a resolver miss; `bridge/handlers.go:804` answers it; resolvers `node/security.go:66`, `bridge/bridgetest/handshake.go:212`. Without a resolver the single constructor identity remains (test path) |
| CASE: attestation challenge one global slot, PASE only | fixed | per-session challenge: `bridge/receive_dispatch.go:510-511` stamps `sessionAttestationChallenge(sessionID)` (`:903-920`, `secure/operational/manager.go:835` `AttestationChallengeFor`) into the invoke ctx; `cluster/core/operational_credentials.go:1294`, `:1461` sign with it; CASE sessions carry theirs (`secure/operational/manager.go:545`). The global `SetAttestationChallenge` (`:201`) remains as the fallback |
| mDNS: commissionable SII default | fixed (batch 1 says so) | `mdns/service.go:372` `defaultCommissionableSII uint32 = 500`, used at `:417` |
| App: CT attributes StartUpColorTemperatureMireds / CoupleColorTempToLevelMinMireds | fixed (batch 3 says so, for the old `internal/model` path; the server now is `cluster/light`) | `cluster/light/colorcontrol_server.go:526` (`AttrCoupleColorTempToLevelMinMireds`), `:532`, `:563` (`AttrStartUpColorTemperatureMireds`, read and write) |

The section's other `bullet` lines carry a FIXED marker already.

## Open questions

1. `LightSensorTypeEnum` `Ms` 64–254 is a range member the brief did not
   list. It is generated, classified in chipdm with the same reason, and in
   the CHANGELOG. Keep it in the "manufacturer range" reason, or give it its
   own wording?
2. `modebase.validHostStatus` accepts any host status `>= 0x80`; the
   snapshot's MfgCodes range is 128–191. Narrow it to the definition
   (`spec.EnumSupported` on ModeChangeStatus, refusing 192–255), or keep it?
   Not changed here: it would refuse statuses that were accepted.
3. §3 writes "(c) not yet run" as the brief says; the document's class
   legend (`docs/certifiability.md:40-43`) defines (c) as "harness: the test
   case or the environment is at fault". Extend the legend, or use another
   label?
4. `ExchangeManager.ts:658` and `OtaSoftwareUpdateRequestorServer.ts:1354`:
   the brief's known resolutions (`:661-664` TCP drop → initiateForceClose;
   `updateQueryInterval = Hours(24)`) match the text at the old line numbers,
   but every Go comment that cites them describes `#sendCloseSession` and the
   persisted `activeOtaProviders`. The citations now point at what the
   comments describe (`:716-726`, `:1354` unchanged). Confirm.
5. The DoorLock comments AD will rewrite still carry `:941`
   (`cluster/lock/doorlock_server.go:354`), which has no single counterpart
   in the rewritten server.
6. `notes/briefs/reports/*.md` were treated as historical (not renumbered),
   like the briefs; `CHANGELOG.md` released entries were renumbered. Flip
   either if that is the wrong reading.
