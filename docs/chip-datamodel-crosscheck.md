# CHIP data model cross-check

The schema snapshot (`parity/schema.json`) and the cluster definitions
generated from it come from matter.js, the gold standard of this module
([ADR 0013](./adr/0013-generated-cluster-definitions.md)). The CSA's Python
certification cases judge a device against something else: connectedhomeip's
spec-derived data model, `data_model/<version>/` (`clusters/*.xml`,
`device_types/*.xml`), read by
`src/python_testing/matter_testing_infrastructure/matter/testing/spec_parsing.py`.
Certifiability is a goal ([ADR 0011](./adr/0011-certifiability-is-a-goal.md),
[`certifiability.md`](./certifiability.md)), so every disagreement between the
two is found by `go test` (with a connectedhomeip checkout, as CI has) before a
certification case finds it — for the whole
snapshot, not only for the clusters the reference daemon mounts.

## How it works

`internal/chipdm` reduces both sides to one model and compares them element by
element, the way matter.js validates its own model against the same XML
(`support/codegen/src/chipdm/`, `validate-chipdm-model.ts`):

- **clusters** — id, name, revision; features (code, bit, conformance);
  attributes, commands and events (id, name, type, direction, response,
  priority, conformance, constraint, access facet by facet, quality, default,
  list entry type); data types and their members (enum values, bitmap bits,
  struct, command and event fields). Derived clusters are compared resolved:
  the snapshot's `effective` layer against CHIP's base chain (`baseCluster`).
- **device types** — id, name, revision, classification, and every server and
  client cluster requirement with its conformance.
- **global data types** — CHIP's `globals/` against the snapshot's
  `globalDatatypes`.

Conformance is compared as an expression, not as text: CHIP's
`<mandatoryConform>` / `<optionalConform>` / `<otherwiseConform>` / … tree and
matter.js's AST (or, for device-type requirements, its text, parsed with
matter.js's grammar) become one tree; names compare case- and
punctuation-insensitively, literals by value, and the operands of an AND / OR
chain as a set.

Every difference is classified, none is tolerated silently:

| Class | Meaning | Where |
| --- | --- | --- |
| (i) | matter.js is right or deliberately different: CHIP's XML lags or is known wrong, or a matter.js override states a reasoned divergence | `internal/chipdm/acknowledged.go`, with matter.js's reason and source |
| (ii) | CHIP is right and the snapshot is wrong — a certifiability risk wherever go-fabric depends on the value | `acknowledged.go`, each with its impact; open ones in [`matter_behaviour_findings.md`](../notes/parity/matter_behaviour_findings.md) |
| (iii) | representation only (naming, how base clusters, global types or supersets are expressed) | normalized in `internal/chipdm/compare.go`, counted per rule |
| (iv) | the certification harness's own data-model parsing corrects or excuses it | `acknowledged.go`, citing `spec_parsing.py` |

`TestChipDataModelCrosscheck` fails on a difference the table does not
explain and on a table entry that explains none, so the table cannot rot.

## How the harness reads the same XML

What `spec_parsing.py`, `conformance.py` and `device_conformance_tests.py`
(at the pinned commit) do with the XML, and so what agreement buys:

- **Directory.** The DUT's `SpecificationVersion` selects the directory
  (`dm_from_spec_version`): 0x01060100 → `data_model/1.6.1`, the directory
  compared here.
- **Derived clusters.** A derived cluster is its base (a pure base cluster
  such as Mode Base or Alarm Base, or another cluster) with the derived
  file's conformance and access overriding (`combine_derived_clusters_with_base`)
  — the resolution `internal/chipdm` applies before comparing. The
  concentration-measurement family is one file with ten cluster ids.
- **Conformance.** D (deprecated) evaluates as disallowed, P (provisional)
  as disallowed unless the run allows provisional elements, `desc` as
  optional. A D or X in CHIP's model where the snapshot says O or M would
  therefore fail TC_DeviceConformance on a device that serves the element —
  the cross-check finds exactly those disagreements.
- **Provisional clusters.** A cluster whose cluster id is marked provisional
  fails the run unless provisional elements are allowed; the generated
  block lists them. go-fabric serves none of them.
- **Known-problem fix-ups** (`build_xml_clusters`): Descriptor's TAGLIST
  feature and every Actions command become optional, ColorControl's
  Primary1–6 attributes optional, TemperatureControl's attributes are
  supplied when the XML lacks them, and AtomicRequest / AtomicResponse are
  added to Thermostat from revision 8. The last is a class (iv)
  acknowledgement; the others cause no difference.
- **Errata overlay** (`data_model/errata_future.yaml`, applied to 1.6 and
  1.7): it sets AmbientContextSensing.SimultaneousDetectionLimit to no write
  access, which the 1.6.1 XML and the snapshot already state.
- **Device types.** The Base Device Type's server clusters are added to
  every device type; superset lines (`classification superset`) govern
  which device types may share an endpoint. Cluster names of 0x0006, 0x0036
  and 0x042A and two device-type names are corrected in code
  (`CLUSTER_NAME_FIXES`, `DEVICE_TYPE_NAME_FIXES`) — names this comparison
  normalizes anyway. The feature, attribute and command overrides a device
  type states for a cluster are enforced (`check_feature_overrides` and its
  siblings); the snapshot does not carry them, so they are the largest part
  of the "Not compared" table below and an open finding in
  [`matter_behaviour_findings.md`](../notes/parity/matter_behaviour_findings.md).

## The CHIP side

The XML is read at test time from a connectedhomeip checkout, at the commit
the harness image was built from (the Makefile's `CHIP_TEST_IMAGE_COMMIT`), in
the directory of the snapshot's Matter revision (`CHIP_DATA_MODEL_VERSION`).
Nothing read from it is committed: every file carries a CSA notice that
forbids publishing it or creating derivative works from it
([ADR 0015](./adr/0015-chip-data-model-read-at-run-time.md)). This page
carries only this module's own analysis — counts, provenance, and each
classified difference as its path and the two differing values.

- The checkout is `GOFABRIC_CHIP_ROOT`, else `../connectedhomeip` beside the
  main checkout (`make chiptool-setup` makes it, with `data_model/<version>`
  in its sparse set). The pinned commit is read with `git archive`, so the
  checkout's own HEAD does not matter.
- Without it, `TestChipDataModelCrosscheck`, `TestChipDataModelPins` and
  `TestCompareSeededDifferences` skip and say why; the parser and comparer
  are still held by unit tests on synthetic fixtures. With
  `GOFABRIC_CHIP_REQUIRED=1` a skip is a failure.
- CI's "chip data model cross-check" job fetches only `data_model/<version>`
  at the pin (a blob-filtered, depth-1 partial clone, cached by pin) and runs
  `make chipdm-check`, so the comparison runs on every pull request.

```sh
make chiptool-setup    # ../connectedhomeip at the pin, data_model/<version> in the sparse set
make chipdm-check      # the cross-check against it; fails instead of skipping
make chipdm-report     # rewrite this page's generated block
```

A schema pin bump, a harness image bump or a reader change re-runs the
comparison; what moved relative to the CSA model is the diff of the generated
block below.

## Status

<!-- BEGIN GENERATED by internal/chipdm TestChipDataModelCrosscheck; do not edit -->

### Provenance

| Side | Source |
| --- | --- |
| snapshot | `parity/schema.json`, matter.js `85cf66472b02763fe3b9c736ebab443b999a95a1`, Matter 1.6.1, SHA-256 `5b57c2878c29bb66e4283607c708f59412bae1cc178d3e618036cc2da95a9bc4` |
| CHIP | connectedhomeip `6170af8461b10b1766044122ac83332c6d00ab20`, `data_model/1.6.1` (git tree `77969732bddf4bd12ba57eb1c6fa032eb2613425`) |
| CHIP's source | specification `1.6.1-attempt-4` (`49f70c101b4211df3febb975a7f7f9d6e4c4bc94`), alchemy version: v1.7.10 |
| read | at test time from a connectedhomeip checkout; nothing of it is committed (ADR 0015) |
| harness | the Makefile's `CHIP_TEST_IMAGE_COMMIT` is the same commit |

### Compared

| Element | Compared |
| --- | ---: |
| attribute | 1116 |
| cluster | 135 |
| command | 457 |
| datatype | 548 |
| deviceType | 91 |
| event | 137 |
| feature | 347 |
| field | 3927 |
| global datatype | 19 |
| requirement | 466 |
| **total** | **7243** |

### Differences by class

| Class | Differences |
| --- | ---: |
| (i) matter.js right or deliberate | 96 |
| (ii) CHIP right, snapshot wrong | 2 |
| (iii) representation, normalized in code | 533 |
| (iv) excused by the harness | 2 |
| unexplained | 0 |

### (iii) Normalization rules

| Rule | Applied | What it normalizes |
| --- | ---: | --- |
| `composed-device-type` | 56 | CHIP models the device types a composed device type contains outside its cluster requirements. |
| `core-global` | 3 | The Interaction Model status codes, event priority and semantic namespace ids are global types of the Core specification that CHIP's data model XML does not restate. |
| `datatype-fabric` | 21 | matter.js marks a fabric-scoped struct by its FabricIndex field, CHIP on the struct. |
| `descriptor-required` | 91 | Every device type requires Descriptor; CHIP's device-type files do not restate it. |
| `enum-as-integer` | 1 | CHIP types a value enumN where the snapshot states the integer of the same width. |
| `fabric-index` | 30 | CHIP omits the FabricIndex of a fabric-scoped value because the specification implies it. |
| `feature-o` | 0 | A feature CHIP marks O where the specification's feature table has no conformance column. |
| `global-attribute` | 231 | Global attributes (0xFFF8 and up) are not restated by CHIP's cluster files. |
| `global-datatype` | 4 | CHIP repeats a global data type in each cluster that uses it; the snapshot defines it once. |
| `member-access` | 0 | The snapshot carries a struct, command or event field's access only where it is fabric-sensitive. |
| `null-default` | 0 | CHIP writes an explicit null default for a nullable value whose default the specification leaves implicit. |
| `quality-reportable` | 0 | CHIP does not model matter.js's P (reportable) quality. |
| `scoped-global` | 0 | CHIP defines the type globally; matter.js scopes it to the cluster that uses it (compared there). |
| `superset-requirement` | 4 | A superset device type: matter.js derives it from the subset type and restates the subset's requirements, CHIP lists only its own. |
| `type-alias` | 18 | The specification and CHIP name the same type differently (matter.js by-design.ts TYPE_ALIASES). |
| `type-bound` | 3 | The snapshot's effective constraint closes a CHIP lower bound with the upper bound of the type (percent 100, percent100ths 10000). |
| `type-metabase` | 28 | CHIP states the primitive or base type a named type builds on (enum8, map8, uint8); the snapshot names the type. |
| `value-table-m` | 43 | An enum value, bitmap bit or status code CHIP marks M where the specification's table has no conformance column. |

### Not compared

What CHIP states and the snapshot does not carry, counted in CHIP's model.

| Aspect | CHIP elements | Why |
| --- | ---: | --- |
| base device type | 4 | CHIP's Base Device Type has no id and the snapshot extracts only device types with one, so its cluster requirements (counted), which the harness adds to every device type, are not compared. |
| cluster classification | 135 | The snapshot does not record a cluster's classification (role / scope). |
| cluster provisional status | 3 | The snapshot does not record that a cluster is provisional; the clusters CHIP marks are listed below. |
| command quality | 39 | The snapshot records no quality for commands (CHIP marks large-message commands L). |
| definitions outside the snapshot | 5 | CHIP global types matter.js defines in a shared definitions scope (WebRtcTransportDefinitions) that the extractor does not emit. |
| device-type cluster quality | 19 | The snapshot does not record the quality of a device type's cluster requirement (singleton). |
| device-type conditions | 46 | The snapshot does not record the conditions a device type declares. |
| device-type element requirements | 153 | The snapshot records a device type's cluster requirements, not its feature / attribute / command / event requirements. |
| event quality | 0 | The snapshot records no quality for events. |
| global commands | 2 | AtomicRequest / AtomicResponse: matter.js models them in the clusters that use them (load-data-model.ts globalCommands). |
| members CHIP does not state | 4 | A command, event or data type whose fields CHIP leaves unstated because the specification gives them by reference (Level Control's *WithOnOff commands); the snapshot's fields have nothing to be compared with. |
| semantic namespaces | 28 | The snapshot carries no semantic namespaces. |

### Provisional clusters

CHIP marks these cluster ids provisional. A certification run rejects a provisional cluster on a device under test (`device_conformance_tests.py` `check_conformance`), whatever the snapshot says.

- TemperatureAlarm
- AmbientContextSensing
- ContentControl

### (i) matter.js is right or deliberately different

| Path | Property | CHIP | snapshot | n | Reason | Source |
| --- | --- | --- | --- | ---: | --- | --- |
| ClosureDimension.Resolution | default | `0.01` | `1` | 1 | CHIP states the fallback of a percent100ths attribute unscaled; 0.01 is not a value of the type | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| ClosureDimension.StepValue | default | `0.01` | `1` | 1 | CHIP states the fallback of a percent100ths attribute unscaled; 0.01 is not a value of the type | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| GeneralCommissioning.ArmFailSafeResponse.ErrorCode | default | `success` | `0` | 1 | The specification names the value Ok where the global status names it Success; spec enhancement filed | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| GeneralCommissioning.SetRegulatoryConfigResponse.ErrorCode | default | `success` | `0` | 1 | The specification names the value Ok where the global status names it Success; spec enhancement filed | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| GeneralCommissioning.CommissioningCompleteResponse.ErrorCode | default | `success` | `0` | 1 | The specification names the value Ok where the global status names it Success; spec enhancement filed | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| GeneralCommissioning.SetTcAcknowledgementsResponse.ErrorCode | default | `success` | `0` | 1 | The specification names the value Ok where the global status names it Success; spec enhancement filed | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| OperationalCredentials.NocResponse.FabricIndex | conformance | `statuscode==success,o` | `statuscode==ok,o` | 1 | The specification names the value Ok where the global status names it Success; spec enhancement filed | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| JointFabricAdministrator.IcaccsrResponse.Icaccsr | conformance | `statuscode==success,o` | `statuscode==ok,o` | 1 | The specification names the value Ok where the global status names it Success; spec enhancement filed | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| DoorLock.DoorStateChange | priority | `desc` | `critical` | 1 | The specification requires CRITICAL for the states that matter and permits INFO otherwise, so matter.js states CRITICAL; a DoorLock override records that deliberately | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| DoorLock.LockOperation | priority | `desc` | `critical` | 1 | The specification requires CRITICAL for the states that matter and permits INFO otherwise, so matter.js states CRITICAL; a DoorLock override records that deliberately | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| DoorLock.LockOperationError | priority | `desc` | `critical` | 1 | The specification requires CRITICAL for the states that matter and permits INFO otherwise, so matter.js states CRITICAL; a DoorLock override records that deliberately | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| DoorLock.ClearWeekDaySchedule.WeekDayIndex | constraint | `254` | `1tonumberofweekdayschedulessupportedperuser,254` | 1 | We keep both alternatives the specification states; CHIP keeps the sentinel | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| DoorLock.ClearYearDaySchedule.YearDayIndex | constraint | `254` | `1tonumberofyeardayschedulessupportedperuser,254` | 1 | We keep both alternatives the specification states; CHIP keeps the sentinel | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| DoorLock.ClearHolidaySchedule.HolidayIndex | constraint | `254` | `1tonumberofholidayschedulessupported,254` | 1 | We keep both alternatives the specification states; CHIP keeps the sentinel | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| DoorLock.ClearUser.UserIndex | constraint | `65534` | `1tonumberoftotaluserssupported,65534` | 1 | We keep both alternatives the specification states; CHIP keeps the sentinel | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| IlluminanceMeasurement.MeasuredValue | constraint | `0` | `0,minmeasuredvaluetomaxmeasuredvalue` | 1 | We keep the range the specification states alongside the zero sentinel | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| Thermostat.SetpointChangeAmount | type | `int16s` | `temperaturedifference` | 1 | We name the temperature type the specification defines; CHIP states a primitive | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| ContentLauncher.ContentSearchStruct.ParameterList | default | `0` | `undefined` | 1 | CHIP states a numeric fallback for a list; the specification states none | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| *.SolicitOfferResponse.VideoStreamId | conformance | `solicitoffer,d` | `solicitoffer.videostreamid,d` | 1 | matter.js keeps the field the conformance selects; CHIP names only the command | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| *.SolicitOfferResponse.AudioStreamId | conformance | `solicitoffer,d` | `solicitoffer.audiostreamid,d` | 1 | matter.js keeps the field the conformance selects; CHIP names only the command | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| *.ProvideOfferResponse.VideoStreamId | conformance | `provideoffer,d` | `provideoffer.videostreamid,d` | 1 | matter.js keeps the field the conformance selects; CHIP names only the command | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| *.ProvideOfferResponse.AudioStreamId | conformance | `provideoffer,d` | `provideoffer.audiostreamid,d` | 1 | matter.js keeps the field the conformance selects; CHIP names only the command | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| KeypadInput.CecKeyCodeEnum.Reserved | field | `present` | `absent` | 1 | We drop a value the specification names Reserved; CHIP keeps it | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| Messages.MessageID | datatype | `absent` | `present` | 1 | The specification defines the type in the cluster; CHIP treats it as a built-in | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| ScenesManagement.LogicalSceneTable | datatype | `absent` | `present` | 1 | The specification defines the type in the cluster; CHIP omits it | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| *.ModeChangeStatus | datatype | `absent` | `present` | 10 | matter.js names the status codes of the mode clusters ModeChangeStatus; CHIP types the field as the global status | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| RvcCleanMode.StatusCodeEnum | datatype | `present` | `absent` | 1 | matter.js names the status codes of the mode clusters ModeChangeStatus | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| RvcRunMode.StatusCodeEnum | datatype | `present` | `absent` | 1 | matter.js names the status codes of the mode clusters ModeChangeStatus | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| Thermostat.MinSetpointDeadBand | constraint | `0to127` | `0to12.7°c` | 1 | We keep the temperature notation of the specification where CHIP states the encoded value | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| OperationalCredentials.AttestationResponse.AttestationElements | constraint | `maxresp_max` | `max900` | 1 | matter.js resolves the RESP_MAX constant the specification defines; CHIP keeps the name | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| OperationalCredentials.CsrResponse.NocsrElements | constraint | `maxresp_max` | `max900` | 1 | matter.js resolves the RESP_MAX constant the specification defines; CHIP keeps the name | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| OtaSoftwareUpdateRequestor.AnnounceOtaProvider | access fabric | `absent` | `F` | 1 | The specification scopes the command to the accessing fabric; CHIP's data model XML does not record it | matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES |
| ColorControl.RemainingTime | constraint | `max65534` | `0to65535` | 1 | 65535 stands for "endless" while a color loop runs | matter.js support/models/src/local/ColorControlOverrides.ts |
| ColorControl.ColorTemperatureMireds | constraint | `max65279` | `colortempphysicalminmiredstocolortempphysicalmaxmireds` | 1 | The physical bounds the specification's prose states, which validate a value against the device | matter.js support/models/src/local/ColorControlOverrides.ts |
| CommodityTariff.TariffComponentStruct.PeakPeriod | default | `0` | `undefined` | 1 | The specification states 0 as the default of a struct-typed field, which no struct can be | matter.js support/models/src/local/CommodityTariffOverrides.ts |
| CommodityTariff.TariffComponentStruct.PowerThreshold | default | `0` | `undefined` | 1 | The specification states 0 as the default of a struct-typed field, which no struct can be | matter.js support/models/src/local/CommodityTariffOverrides.ts |
| FanControl.FanModeEnum.Medium | conformance | `[low]` | `o` | 1 | "[Low]" names another enum value, which a value's conformance cannot be evaluated against | matter.js support/models/src/local/FanControl.ts |
| IlluminanceMeasurement.LightSensorType | type | `lightsensortypeenum` | `uint8` | 1 | The specification defines LightSensorTypeEnum but permits values outside it; uint8 is the same wire type | matter.js support/models/src/local/IlluminanceMeasurementOverrides.ts |
| JointFabricDatastore.DatastoreAccessControlEntryStruct.Subjects | constraint | `maxsubjectsperaccesscontrolentry` | `none` | 1 | The bound is an attribute of another cluster (Access Control), which a constraint cannot reach | matter.js support/models/src/local/JointFabricDatastoreOverrides.ts |
| JointFabricDatastore.DatastoreAccessControlEntryStruct.Targets | constraint | `maxtargetsperaccesscontrolentry` | `none` | 1 | The bound is an attribute of another cluster (Access Control), which a constraint cannot reach | matter.js support/models/src/local/JointFabricDatastoreOverrides.ts |
| LocalizationConfiguration.ActiveLocale | constraint | `max35` | `insupportedlocales` | 1 | Validates the value against SupportedLocales, whose entries the specification bounds to 35 | matter.js support/models/src/local/LocalizationConfigurationOverrides.ts |
| OtaSoftwareUpdateRequestor.UpdateStateProgress | quality | `nullable` | `nullable quieter` | 1 | The specification asks nodes not to over-report progress; Q states that | matter.js support/models/src/local/OtaSoftwareUpdateRequestor.ts |
| UserLabel.LabelList | constraint | `desc` | `min0` | 1 | "Minimum 4" means a node supports at least four entries; an empty list is valid | matter.js support/models/src/local/UserLabelOverrides.ts |
| TlsClientManagement.FindEndpointResponse.Endpoint | constraint | `0to65534` | `none` | 1 | The specification bounds a TLSEndpointStruct by the range of TLSEndpointID; no comparison orders a struct against a number | matter.js support/models/src/local/TlsClientManagementOverrides.ts |
| DoorLock.OperatingModesBitmap.AlwaysSet | field | `absent` | `present` | 1 | The bitmap is inverse; the bits the specification leaves unnamed are always set | matter.js support/models/src/local/DoorLockOverrides.ts |
| Thermostat.HVACSystemTypeBitmap | datatype | `absent` | `present` | 1 | Types of the attributes Matter 1.5.1 removed, kept for clients until 1.7 | matter.js support/models/src/local/ThermostatOverrides.ts |
| Thermostat.ProgrammingOperationModeBitmap | datatype | `absent` | `present` | 1 | Types of the attributes Matter 1.5.1 removed, kept for clients until 1.7 | matter.js support/models/src/local/ThermostatOverrides.ts |
| WindowCovering.TypeEnum.* | conformance | `m` | `!tl&lf` | 7 | matter.js states the lift / tilt feature each covering type needs; the specification's enum table has no conformance column, so CHIP marks every value M | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.TypeEnum.* | conformance | `m` | `!lf&tl` | 1 | matter.js states the lift / tilt feature each covering type needs; the specification's enum table has no conformance column, so CHIP marks every value M | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.TypeEnum.* | conformance | `m` | `!lf&tl\|!tl&lf` | 1 | matter.js states the lift / tilt feature each covering type needs; the specification's enum table has no conformance column, so CHIP marks every value M | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.TypeEnum.* | conformance | `m` | `o` | 1 | matter.js states the lift / tilt feature each covering type needs; the specification's enum table has no conformance column, so CHIP marks every value M | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.EndProductTypeEnum.* | conformance | `m` | `!tl&lf` | 15 | matter.js states the lift / tilt feature each covering type needs; the specification's enum table has no conformance column, so CHIP marks every value M | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.EndProductTypeEnum.* | conformance | `m` | `!lf&tl` | 1 | matter.js states the lift / tilt feature each covering type needs; the specification's enum table has no conformance column, so CHIP marks every value M | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.EndProductTypeEnum.* | conformance | `m` | `lf&tl` | 5 | matter.js states the lift / tilt feature each covering type needs; the specification's enum table has no conformance column, so CHIP marks every value M | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.EndProductTypeEnum.* | conformance | `m` | `!lf&tl\|!tl&lf` | 3 | matter.js states the lift / tilt feature each covering type needs; the specification's enum table has no conformance column, so CHIP marks every value M | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.TypeEnum.TiltBlindLift | field | `absent` | `present` | 1 | Value 8 keeps its earlier name; CHIP names it TiltBlindLiftAndTilt (same value) | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.TypeEnum.TiltBlindLiftAndTilt | field | `present` | `absent` | 1 | Value 8 keeps its earlier name TiltBlindLift (same value) | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.GoToLiftPercentage.Ignored | field | `absent` | `present` | 1 | The specification defines a second field CHIP does not use; matter.js disallows it (X) to follow the de-facto standard | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| WindowCovering.GoToTiltPercentage.Ignored | field | `absent` | `present` | 1 | The specification defines a second field CHIP does not use; matter.js disallows it (X) to follow the de-facto standard | matter.js support/models/src/local/WindowCoveringOverrides.ts |
| ModeSelect.StandardNamespace | type | `enum16` | `namespace` | 1 | The namespace type enumerates exactly the standard namespace ids, at the width of the semantic tag's NamespaceID (enum8), which every standard namespace id fits; TLV carries the value at its own width | matter.js support/models/src/local/ModeSelectOverrides.ts |
| WindowCovering.MovementStatus | datatype | `absent` | `present` | 1 | The formal type of the OperationalStatus bit fields, which the specification describes in prose | matter.js support/models/src/local/WindowCoveringOverrides.ts |

### (ii) CHIP is right, the snapshot is wrong

- **GroupKeyManagement.GroupKeySetStruct.GroupKeyMulticastPolicy** (conformance): CHIP `d`, snapshot `o` — 1 difference(s).
  - Why: Matter 1.6.1 deprecates the field (matter.js's own 1.6.1 scrape states D); a matter.js override written when the specification said "P, M" forces O for every revision.
  - Source: matter.js support/models/src/local/GroupKeyManagementOverrides.ts; support/models/src/v1.6.1/spec.ts GroupKeySetStruct.
  - Impact: No certification case reads the field. go-fabric's KeySetReadResponse reports field 8 as matter.js's GroupKeyManagementServer does while its model defines the field, which a D field still is, so the behaviour stays; CHIP's server omits it. Finding recorded; upstream candidate (gate the override until 1.6.1).
- **semtag.Label** (conformance): CHIP `mfgcode!=null,o`, snapshot `o` — 1 difference(s).
  - Why: The specification makes Label mandatory when MfgCode is not null; matter.js relaxes it to O ("TODO we do not support MfgCode != null").
  - Source: matter.js support/models/src/local/semtag.ts.
  - Impact: go-fabric serves no Descriptor TagList (cluster/core/descriptor.go) and ModeSelect's SemanticTagStruct is a different struct, so nothing depends on it. Finding recorded; known upstream TODO.

### (iv) the harness excuses it

| Path | Property | CHIP | snapshot | n | Reason | Source |
| --- | --- | --- | --- | ---: | --- | --- |
| Thermostat.AtomicRequest | command | `absent` | `present` | 1 | CHIP defines AtomicRequest globally (globals/Commands.xml); the harness adds it to Thermostat from revision 8 with conformance Presets \| Schedules and Operate privilege, as matter.js models it there (PRES \| MSCH, the features those attributes require) | connectedhomeip src/python_testing/matter_testing_infrastructure/matter/testing/spec_parsing.py build_xml_clusters ("Need automated parsing for atomic attributes"); matter.js support/models/src/local/ThermostatOverrides.ts |
| Thermostat.AtomicResponse | command | `absent` | `present` | 1 | CHIP defines AtomicResponse globally (globals/Commands.xml); the harness adds it to Thermostat from revision 8 with conformance Presets \| Schedules and Operate privilege, as matter.js models it there (PRES \| MSCH, the features those attributes require) | connectedhomeip src/python_testing/matter_testing_infrastructure/matter/testing/spec_parsing.py build_xml_clusters ("Need automated parsing for atomic attributes"); matter.js support/models/src/local/ThermostatOverrides.ts |

<!-- END GENERATED -->
