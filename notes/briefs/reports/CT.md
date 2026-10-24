# Report CT — M4 phase 21d: the eight test ports whose subject exists in part, and the validator hooks they need

Brief: `notes/briefs/2026-10-24-CT-m4-phase-21d-m5-ports.md`, read map
`notes/briefs/reads/read-m5-ports-in-m4.md`. Branch `m4/phase-21d` from
`origin/main` `7129df88` (CC, #252), `origin/main` `1156f32d` (release
v0.10.7, #253) merged in. matter.js pin `862ad75f`.

## Hooks

| § | Site | Citation (862ad75f) | Rule |
| --- | --- | --- | --- |
| 1.1 | `cluster/spec/markchanged.go` `Server.MarkChanged`, `MarkChangedOf`, `ChangeMarker`; `script/clustergen/hull.go` emits `<cluster>def.Server.MarkChanged` (regenerated) | `ServerBehaviorBacking.ts:186-221`, `BehaviorBacking.ts:440-444`, `Datasource.ts:151` | An empty list, an attribute not served, a changes-omitted ("C") or quieter ("Q") one is refused with matter.js's reason; otherwise the version advances once and the change is notified as `contract.OriginHostSet`. |
| 1.2 | `cluster/spec/patch.go` `Instance.PatchDefaults`, applied in `Server.check` (Initial and the host's Set); `EmptyListDefault`; `script/clustergen/gen.go` `defaultLiteral`/`encodedUnit` (regenerated) | `ValuePatcher.ts:17-233`, `SelectDefaultValue.ts:39-58`, `Scope.ts:202-217`, `DefaultValue.ts:55-59`, `EncodedValue.ts:21-43`, `FieldValue.ts:227-256` | A struct field the host leaves out gets its stated default only when its conformance is mandatory under the selected features (else the null of a nullable field), through list entries and nested structs, copied before it is set; an undeclared Go field is refused. |
| 1.3 | `cluster/spec/field_conformance.go` (compiler port, `Instance.CheckConformance`, `checkResponse`), `write.go` `checkStruct`/`checkValueIn`, `server.go` `MatterWrite`/`MatterInvoke` | `conformance-compiler.ts:87-220` (and its node builders, cited per function), `conformance-util.ts:21-258`, `conformance.ts:19-60`, `ValueValidator.ts:392-467`, `NameResolver.ts:88-105`, `errors.ts:99-123`, `ProtocolService.ts:601-626`, `:681-684`, `:733-740` | Each struct field's conformance is evaluated with the struct as the record (sibling, else cluster attribute, else the outer resolver), the features and the revision; mandatory-absent, disallowed-present, a disallowed or undefined enum value and a choice count are CONSTRAINT_ERROR. A command response missing a field its conformance requires is FAILURE. |
| 1.4 | `endpoint/devicetype_validation.go` `DeviceTypeValidator.judge`, `affectedByAdditions`, `firstMisplacing`; `devicetype_conformance.go` `dtPass.judged` | `DeviceTypeConformanceService.ts:159-173`, `:295`, `:335-338`, `:384-454`, `:371-382`, `:480-484`, `:525-619` | Strict refuses a new violation only on an endpoint an addition may affect (the added subtree, its ancestors, its siblings); one a change of an installed endpoint caused is logged. A singleton refusal names the first misplacing endpoint alone. Endpoints outside a node scope are not judged. |
| 1.5 | `cluster/spec/server.go` `UnimplementedMandatoryCommands`; `endpoint/devicetype_subject.go` `ClusterShape.UnimplementedCommands`, `unimplementedOf`; `ViolationUnimplemented`; `node/config.go` mode check | `ValidatedElements.ts:26-37`, `:154-180`, `:357-372`, `ServerBehaviorBacking.ts:100-102`, `DeviceTypeConformanceService.ts:674-685` | A mandatory command a server leaves unimplemented is a `ViolationUnimplemented` the validator reports with the endpoint's violations: logged once in warn and off, refused in strict where an addition caused it. `node.Config.Validate` refuses a mode that is none of the three. |

The log-once rule (`RuntimeValidationTest.ts:320`) needed no fix: the
recorded set is replaced each pass, so a violation that went away and
returns is fresh again; the port case passes.

## Ports

| matter.js file | Go | Ported / skipped | Register |
| --- | --- | --- | --- |
| `MarkChangedTest.ts` | `node/mark_changed_parity_matterjs_test.go` | 9 / 1 | partial |
| `ValuePatcherTest.ts` | `cluster/spec/value_patcher_parity_matterjs_test.go` | 8 / 0 | ported |
| `conformanceTest.ts` | `cluster/spec/conformance_parity_matterjs_test.go`, `testdata/conformance_matterjs.json` | 181 (170 table + 11 explicit) / 5 | partial |
| `EndpointNumberConflictTest.ts` | `node/endpoint_number_conflict_parity_matterjs_test.go` | 11 / 11 | partial |
| `ProtocolServiceTest.ts` | `node/protocol_service_parity_matterjs_test.go` | 9 / 4 | partial |
| `ConstructionValidationTest.ts` | `endpoint/construction_validation_parity_matterjs_test.go` + `node/devicetype_validation_parity_matterjs_test.go` | 22 / 8 | partial |
| `RuntimeValidationTest.ts` | `endpoint/runtime_validation_parity_matterjs_test.go` + `node/devicetype_validation_parity_matterjs_test.go` | 43 / 27 | partial |
| `StandardDeviceTypeCorpusTest.ts` | `device/standard_device_type_corpus_parity_matterjs_test.go` | 76 / 17 | partial |

`testparityPlannedRatchet` 35 → 27, one step per commit.

Skipped, with the reason the test states:

- **MarkChanged** — "advances the data version when a computed quieter attribute reports through its throttle": `spec.Server` has no throttled report path for a quieter attribute served from a Source; the self-held servers bump at the change.
- **conformance** — the "operational extension" describe (5): `ClusterModel.extend` has no Go subject; the generated definitions do not extend one another.
- **EndpointNumberConflict** — the claimant describes and cases (10): a host claims no endpoint number; the assembler allocates each from the persisted counter. "does not list a refused child of a parent without an index": a refusal is atomic per reassembly, a refused part refuses its parent.
- **ProtocolService** — `skipCustomElements` (wildcard path flags not in the Go IM); the two transaction cases (no transaction model); "rejects request with constraint violation" (see Differences).
- **ConstructionValidation** — two crashed-endpoint cases; three construction-phase cases (an endpoint is in a snapshot whole or not at all); two peer cases; BLE (ADR 0024).
- **RuntimeValidation** — the counting cases (judged/read endpoints, the node-scope describe, the bridged-light/lock constants; 17), crash (2), throwing judgement (2), factory reset of a followed list, teardown, peers (2), the off-mode observers, and "is not refused for a singleton a runtime change misplaced in an ancestor" (ADR 0016).
- **StandardDeviceTypeCorpus** — the Closure TagList refusal (no TagList declaration, M5); the 12 names devicegen does not commit (its reason); RootNode-as-root, Aggregator, BridgedNode, OtaRequestor (the fixture table's reason).

Where a matter.js case asserts a verdict and a count, the verdict ported and
the count is left with a comment.

## Guards

Summary log: one exit line per guard, HEAD per guard.

| Guard | HEAD | Result |
| --- | --- | --- |
| `make generate-matter-schema` (`MATTERJS_DIR` at the pin; §1.1 and §1.2 changed the generator) | `64379c6d` | exit 0, no file changed |
| `make ci` | `c50472f7` | exit 0 (1337 s). The first run at `64379c6d` exited 2: `cluster/spec` coverage 92.2 % < floor 97 %. Fixed by `76d66e22` (unit cases of the hooks, 98.0 %) |
| `make race` (twice: MarkChanged notifies from a host goroutine) | `c50472f7` | exit 0 (670 s), exit 0 (363 s), no DATA RACE |
| `make reachability`, then `make reachability-check` | `76d66e22` (inventory committed in `c50472f7`), `c50472f7` | exit 0, exit 0 |
| `make cite-report`, `make cite-check` (`MATTERJS_DIR` set) | `c50472f7` | exit 0. The first check found 5 ambiguous or out-of-range citations and 3 drifts in the new code; all fixed in `c50472f7`, drift count unchanged |
| `make testparity`, `make testparity-check` | `c50472f7` | exit 0, exit 0 (planned 27) |
| `go test ./script/...` (clustergen included) | `c50472f7` | exit 0 |
| `mkdocs build --strict` (`docs/guides/ports.md` changed) | `c50472f7` | exit 0 |

The merge of `origin/main` (`1156f32d`, release v0.10.7) put my CHANGELOG lines into `[0.10.7]`; `64379c6d` moves them back under `[Unreleased]`.

## Differences

- **§1.3 is narrower than the brief.** Field conformance runs on a generic `spec.Server`'s controller write and on the presence of a command response's fields. It does not run on the host's `Initial`/`Set` or in the checks self-held servers call (`CheckSet`, `CheckValue`, `ValidateWrite`). Enforcing it there refused the reference bridge's CHIP-mirrored samples: CommodityTariff `PowerThresholdStruct` sets both `O.b` fields (`examples/reference-bridge/fleet_energy_market.go:381`, CommodityTariffSamples.h), and the DEM forecasts omit `LatestEndTime` under STA and `MinPowerAdjustment` under FA & PFR (`fleet_evse.go:669-691`). The published tariff and forecasts would have changed, so this went to Questions.
- **No request refusal.** Refusing a request field's constraint generically (matter.js refuses through the request TLV schema, `CommandInvokeResponse.ts:484-487`, `TlvOfModel.ts:336-339`) broke ported expectations: OnOff `OnWithTimedOff` OnTime 0xFFFF, the DoorLock enum refusals as INVALID_COMMAND, EVSE RESOURCE_EXHAUSTED, WaterHeater and CommodityPrice INVALID_COMMAND. matter.js logs a request's conformance failure (`ProtocolService.ts:662-679`).
- **Response check is presence-only and top-level.** DoorLock echoes an out-of-range index with its constraint switched off (`DoorLockServer.ts:704-707`) and answers DUPLICATE in the global status enum (`:441`). A full check refused both.
- **§1.2 needed the generator.** Unit defaults (`{"type":"celsius","value":26}`) were not rendered. They are restated in encoding units now, and a stated `[]` is `spec.EmptyListDefault`. Regenerated with `go run ./script/clustergen`; `make generate-matter-schema` at the pin changed nothing more. Thermostat setpoint limits and PresetStruct setpoints and ClosureDimension Resolution/StepValue gained defaults. Their reference-bridge servers are self-held, so the publication is unchanged.
- **§1.4 approximates `#affectedBy`.** The node-scope reach (`Reach.NodeEndpoint`, `Reach.NodeScope`) is not modelled. A new violation an addition causes on an unrelated endpoint through the node scope is logged, not refused. ADR 0016's misplaced-singleton refusal in every mode stays, where matter.js logs a singleton a runtime change misplaces (`:295` `refuse: false`).
- **§1.5 kind.** matter.js reports an unimplemented mandatory command as a `ClusterElementError` when it constructs the behavior, not as a device-type violation. `ViolationUnimplemented` is reported by `DeviceTypeValidator`, not by `ValidateDeviceTypes`, so `endpointtest`'s conformance assertion is unaffected. Off mode reports it, as matter.js warns in any mode.
- **One file per matter.js file, twice bent.** The two validation files have their whole-node cases (strict node, unknown mode, default configuration) in `node/devicetype_validation_parity_matterjs_test.go`. An internal `endpoint` test cannot import `node`. The endpoint file's `t.Run` for each skips with "ported as node:…", and the register names both tests.
- matter.js logs one line per endpoint; the bridge logs one `matter.devicetype.violation` record per violation. The ports group the verdict into matter.js's lines.
- A refusal names its endpoints in tree order, where matter.js names the added endpoint first (`RuntimeValidationTest.ts:1102`).
- The read map's conformance tree starts at `conformanceTest.ts:49`, not `:58`. The rest of its lines checked out where used.
- The coverage fix found a defect in §1.2's first form: a pointer field holding a nested struct was patched in place, writing through to the host's value. It now gets a pointer to the patched copy (`76d66e22`, `TestPatchBranches`). An enum member whose conformance evaluates to a value counts by whether that value is defined; matter.js throws `UnsupportedConformanceNodeError` there (`conformance-compiler.ts:829-830`).
- `GOTMPDIR` is `/tmp/claude-1000/ct-gotmp`. The sandbox refused commands whose environment named the scratchpad path.

## Decisions

- MarkChanged reports as `contract.OriginHostSet`; no new origin.
- PatchDefaults is its own `Instance` method run in `Server.check` before the value checks, not inside `checkStruct`, which also serves the wire write the brief keeps unpatched.
- The conformance table is extracted from the test file at the pin by `notes/parity/matter/extract-conformance-cases.js` (plain node) into `cluster/spec/testdata/conformance_matterjs.json`. Conformance text is parsed with `internal/chipdm.ParseConformance`; record fields are `*any`, so undefined and null stay apart.
- The validation ports judge fake trees through `DeviceTypeValidator.judge`, the validator core `Validate` now delegates to (tree shape + violations). The synthetic models are schema definitions.
- `ValidateDeviceTypes` skips endpoints no node endpoint is above (matter.js `#judge`); `DeviceTypeTree.Check` still judges any.
- The corpus builds each type without its fixture's parts (matter.js judges the generated type) and reads devicegen's `excluded` map from its source with `go/parser`.
- The ProtocolService TestCluster is registered once in the `node_test` package (`spec.Register` in `init`), so the bridge decodes its request.

## Left out

- Field conformance on the host's values and in self-held servers (Questions).
- Request constraint and conformance refusal; a response's enum values and nested structs.
- The node-scope reach of `#affectedBy`.
- MarkChanged's throttled quieter path.
- No chip-tool run. The composition, the FeatureMaps and the published values of the reference bridge did not move: the new defaults land on generic servers the bridge serves self-held, and the hooks refuse or log. No chip-tool label.
- No `by_design.md` entries; the candidates are below.

## Questions

1. Should field conformance hold the host's own struct values and those of self-held servers, as matter.js validates every commit? That refuses the reference bridge's CHIP-mirrored tariff thresholds (both `O.b`) and DEM forecasts. Either the samples change (published tariff and forecast change; a chip-tool families run of SETRF and DEM decides) or the host path stays unchecked.
2. Should a generic server refuse a request field outside its constraint with CONSTRAINT_ERROR, as matter.js's request TLV schema does over the wire? The OnOff, DoorLock, EVSE, WaterHeater and CommodityPrice ports pin handler statuses for such requests today.
3. ADR 0016 refuses a misplaced singleton in every mode, a runtime change's included. matter.js logs the latter. Keep?

## by_design candidates

- **Operational extension.** `conformanceTest.ts:1216-1264` validates through `ClusterModel.extend`. The generated definitions do not extend one another, so the base's conformance is the definition's.
- **The whole-tree pass.** The counting cases of `RuntimeValidationTest.ts` (judged/read endpoints, NodeScopeIndex, `:577`, `:617`, `:634-652`, `:655-778`, `:781`, `:969-1005`) and `countingValidations` (`ConstructionValidationTest.ts:101-130`) have no subject. The Go pass is linear in the tree per reassembly and keeps no index (ADR 0016, `BD-Matter-DeviceTypeValidationPerTopology`).
- **A request's constraint is the handler's.** matter.js refuses over the wire through the request TLV schema's bounds (`CommandInvokeResponse.ts:484-487`). The Go servers answer as matter.js's behaviors do when called directly.
- **A response is held to field presence only.** `ProtocolService.ts:725-740` validates the whole payload. DoorLock switches the constraint off per response (`DoorLockServer.ts:704-707`); the Go servers have no such switch.
- **The singleton in every mode.** `DeviceTypeConformanceService.ts:295`, `:335-338` judge runtime changes with `refuse: false`. ADR 0016 refuses a misplaced singleton whenever it appears.
- **An unimplemented mandatory command as a validator violation.** matter.js: `ValidatedElements.ts:357-372`, a behavior error at construction. Go: `ViolationUnimplemented` in the device-type verdict, where the mode already is.
