# Report O — harness follow-ups of wave 4, the second ESA, the wire-shape guard, SU / BDX plumbing

Branch `wave5/harness` (from `origin/main` 07d9622), no PR. chip sources
were read at the harness pin
`git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`;
line numbers below are the pin's.

## What was built

| Commit | Must | What |
| --- | --- | --- |
| `test: deprecation policy …` | 8 | `deprecatedIdentifiers` reads every member of a `const (` / `var (` group whose doc carries `Deprecated:`, and an indented marker inside a group. `TestDeprecatedConstGroupIsChecked` holds it to the five ModeSelect constants (`cluster/modeselect/modeselect_server.go:88-97, 128-135`) and a synthetic source. `looseDeprecation` stays top-level: indented, it fires on prose (`fancontrol_server.go`, `windowcovering_server.go`). |
| `reference-bridge: a second ESA …` | 2 | `fleet_esa.go`: `newStateForecastDem` is `newDemoDem` with SFR\|STA\|PAU\|FA\|CON (0x7C — bits 2-6, `cluster/spec/deviceenergymanagement/definition_gen.go:69-73`; `TC_DEM_2_6.py:30`, `TC_DEM_2_8.py:30`), same ESA type, power range, delegate, modes and triggers; a standalone bridged device "Charger ESA" stating ControllableEsa, appended after the grid meter. Wiring: three lines in `fleet.go` (field, constructor, `append(f.energyMarketSpecs(spec), f.esaSpec(spec))`) and the dispatch line in `control.go` (`f.esa.testEventTrigger(trigger) \|\|` before the charger). The ESA handles a 0x0098… trigger under its own lock and returns false, so the charger's part answers it too (report K's design). |
| `chiptool: per-case endpoints …` | 1, 4, 7 | see below |
| `reference-bridge: fleet-wide wire-shape guard` | 3 | `wire_shape_test.go` |
| `docs: CHANGELOG …` | 9 | `[Unreleased]` → Added |
| `chiptool: an image without ota_image_tool.py …` | 7 | the missing tool is recorded, not a family failure |

Each Go-changing commit carries its regenerated reachability inventory.

### Harness (Must 1, 4, 7)

- **Must 1 — per-case endpoint.** The table already had the mechanism:
  `caseDeviceType` (`families_table_test.go:64-67` before this branch),
  read by `deviceTypeOf` (`familyrun_test.go:692-697` on origin/main, :759 now), which both the
  Python (:348-363) and the YAML (:365-367) arms (origin/main) use to pick the endpoint
  **and** `pics.byEP[ep]`; `skipNotApplicable` judges with that endpoint's
  values. So I renamed the field to the brief's `endpoints` (its one use,
  TC-S-2.2/2.4, follows) instead of adding a second field with the same
  meaning. The device-type list moved out of the tagged runner into
  `endpointDeviceTypes` and `pseudoEndpoints`; `devicePICS` resolves both.
  `dtStateForecastEsa = dtElectricalMeter<<16 | dtDeviceEnergyManagement`
  resolves with `endpointAfter(dtElectricalMeter, 0x050D)` (the meter's key
  in the high half, as `dtOvenCavity` carries its anchor; report K had
  proposed `DEM<<16|DEM`). DEM row: `endpoints: {"2.6", "2.8": dtStateForecastEsa}`.
  `epOf` and the workflow group for DEM unchanged.
- **Must 4 — EGC** on `dtElectricalEnergyTariff`, group `app-energy`.
  `TC_EGC_2_3.py:61-64` gates on `"EGC.F00"`, a code no PICS defines (the
  device-generated slices name features `EGC.S.F00`, e.g. `testdata/pics/ep0.txt`;
  `ci-pics-values` at the pin has no EGC line), so the row carries a
  `picsEdits` + `edits` pair `EGC.F00` → `EGC.S.F00`, the pattern of the
  CLCTRL/5.1 row. No matter.js edit or upstream issue exists to cite
  (matter.js chip-testing does not run EGC) — see question 3.
- **Must 7 — SU and BDX.** SU already had a row (core). BDX added next to
  it (core). The plumbing (`otaplumbing_test.go`, untagged; `prepareOta`
  in `familyrun_test.go`, tagged):
  - the image: `python3 /src/app/ota_image_tool.py create -v 0xfff1 -p 0x8001 -vn 2 -vs 2.0 -da sha256 <payload> <image>`,
    transcribed from chip's own caller `scripts/build/build_ota_images.sh:106`
    (argument set: `src/app/ota_image_tool.py:326-345`); vendor/product
    from `examples/reference-bridge/main.go:53-54`, version = BasicInformation
    SoftwareVersion 1 (`wiring.go:107`) + 1. Tool path from matter.js HEAD
    `support/chip/Dockerfile:234`.
  - the provider: `which chip-ota-provider-app` in the container
    (`Dockerfile:159` builds it).
  - the arguments: every `provider_app_path:` / `ota_image:` value of a
    case's descriptor arguments is replaced (`TC_SU_2_2.py:39-40`,
    `TC_SU_2_4.py:39,42`, `TC_SU_2_5.py:40-41`, `TC_SU_2_7.py:41,44`);
    `ota_provider_port:5541`, the provider pipes and timeouts stay as the
    headers give them (daemon is on 5540, no clash).
  - PIXITs: none — no `PIXIT` appears in `TC_SU_*.py` at the pin.
  - `prepareOta` runs once per harness for every family with such a case,
    whether or not the case applies, so CI exercises it before brief N.
    A tool that exists and fails fails the family; a missing tool or
    provider is logged and makes an applicable case fail naming it.
  - PICS untouched: `MCORE.OTA.Requestor` is a generated code
    (`reGeneratedCode`, `familyrun_test.go:118`), 0 until N mounts the
    requestor, so SU/2.2, 2.4, 4.1 stay not applicable (golden
    `notApplicable`).
  - BDX at the pin: `Test_TC_BDX_1_1…2_5.yaml` only, every step
    `disabled: true` (e.g. `Test_TC_BDX_2_1.yaml:31,54,69,94`); no Python
    BDX case. They will be manual in the descriptor (inferred — see question 4).

### Wire-shape guard (Must 3)

`TestFleetWireShapes` reads every attribute of every endpoint with one
wildcard read through `br.Dispatcher()` (the `ReadResult` values the
bridge hands its writer), once at start and once after firing the triggers
that fill list/struct attributes (tariff, price, grid, metering, meter
identification, DEM forecast/power adjustment/opt-out, EVSE, water heater,
smoke, BooleanStateConfiguration). It fails on (a) a value whose type the
writer's switch does not know — it goes on the wire as null
(`bridge/reply.go` default arm after `encodeApplicationValue`) — and (b) a
null where `parity/schema.json` (`effective.quality.nullable`) says the
attribute is not nullable. The mechanism is checked on the defect class:
`[]ct.DayEntryStruct` is refused, `spec.List[ct.DayEntryStruct]` accepted.

Measured: 2154 attributes per pass, **0 findings** in both passes. So no
server needed a `spec.List` fix and there are no per-package fix commits.
The brief's candidates are all known to the writer today: Descriptor
DeviceTypeList (`[]mattercore.DeviceTypeStruct`), `[]mattermeasure.AccuracyStruct`
(both `bridge/reply.go` top-level cases), OperationalStateList and ModeBase
SupportedModes (`bridge/application_values.go:33-38`).

The writer is unexported, so `writerEncodes` transcribes its type switch
(`reply.go:650-1147`, `application_values.go:26-48`, `encodeScenesResponse`).
That copy can drift — see question 1.

### TagList (Must 5) — not servable; entry for the owner

`cluster/core/descriptor.go` cannot serve it: the instance is built with
no feature (`:93-95`), `MatterRead(TagList)` returns `(nil, false)`
(`:177-186`) and `MatterAttributes` omits it (`:231-237`), both for the
Apple `semtag` rejection that `BD-Matter-ClosureWithoutTagList` records.
matter.js requires TAGLIST on ElectricalEnergyTariff only
(`packages/model/src/standard/elements/electrical-energy-tariff.element.ts:21`,
`packages/node/src/devices/electrical-energy-tariff.ts:32`); the
ElectricalMeter files carry no TagList requirement (grep of
`electrical-meter.element.ts` and `devices/electrical-meter.ts`: none). No
code changed; `TestEnergyMarketDevices` keeps tolerating exactly that one
finding.

### chipdm (Must 6) — nothing to classify

`make chipdm-check CHIP_ROOT=<main checkout>` green (it reads the pin's
`data_model/1.6.1` through git); `make chipdm-report` leaves
`docs/chip-datamodel-crosscheck.md` unchanged. The cross-check compares
cluster revisions; CHIP's data model says CommodityTariff revision 1
(`data_model/1.6.1/clusters/CommodityTariff.xml:60,62`), as the snapshot
does. The energy-gateway app's zap value 2 (report K, question 4) is not
the data model the Python cases judge by, and the cross-check reports no
difference — `internal/chipdm/acknowledged.go` is untouched.

## Guards

| Guard | Result |
| --- | --- |
| `go test . ./internal/chiptool/ ./examples/... ./cluster/... ./schema/...` | green except `TestCertifiabilityDocument` (expected: "family BDX / EGC is declared but has never run") |
| `go test ./internal/chiptool/ -run 'TestWorkflow\|TestFamil'` | green (`TestWorkflowRunsEveryFamily`, new `TestFamilyDeviceTypesResolve`); `TestOtaPlumbing` green; `go vet -tags chiptool ./internal/chiptool/` clean |
| `make lint` | 0 issues (both runs), tidy/verify clean |
| `make cover-check` | red only on the two expected: `TestCertifiabilityDocument` and the darwin mdns `TestBuildOperationalService_EmptyHostName_UsesDefault` |
| `make reachability-check` | green |
| `make chipdm-check` | green |
| race: `.`, `examples/reference-bridge` | green |

What proves what: `TestStateForecastEsa` (FeatureMap 0x7C, ControllableEsa,
DEMM mounted, no device-type violation, numbered after meter and charger
with no 0x050D endpoint between — what `endpointAfter` relies on);
`TestDemTriggerReachesBothEsas` (one DEM trigger fills both forecasts; the
ESA's slots carry ManufacturerESAState, the charger's do not);
`TestFleetWireShapes`; `TestFamilyDeviceTypesResolve` (every row and
`endpoints` value resolves, every `endpoints` key is a case of the
family in the golden); `TestOtaPlumbing` (identity read from the daemon's
source, rewrite on TC_SU_2_2's header, the exact image command);
`TestDeprecatedConstGroupIsChecked`.

`docs/certifiability.md` was regenerated (`-update-certifiability-doc`):
it gains the BDX and EGC rows with zero counts.

## Left out, and why

1. **No server `spec.List` fixes** — the guard found nothing to fix (above).
2. **TagList** — the Descriptor cannot serve it (above); an entry for the owner.
3. **The SU requestor cases do not run** — brief N; their PICS gates stay honest.

## Open questions

1. **An exported writer predicate (brief M).** The guard transcribes the
   bridge writer's type switch. A `bridge` function such as
   `func EncodesAttributeValue(v any) bool` (true when
   `defaultAttributeValueWriter` writes `v` as itself), or the writer
   itself exported, would let `TestFleetWireShapes` call it and drop the
   copy.
2. **`endpoints` is the renamed `caseDeviceType`**, not a second field —
   accepted? And the pseudo key `dtElectricalMeter<<16|dtDeviceEnergyManagement`
   rather than report K's `DEM<<16|DEM`?
3. **The EGC/2.3 edit** (`EGC.F00` → `EGC.S.F00`) has no matter.js edit or
   upstream issue behind it, only the case line and the PICS naming. Keep
   it, or let 2.3 stay not applicable and file upstream? Unverified: that
   the image's descriptor carries the expression with `EGC.F00` (the
   runner reports a mismatch as an error naming the expression).
4. **BDX in the descriptor (inferred, unverified).** The pinned image's
   descriptor lists SU's YAML cases from `src/app/tests/suites/certification`
   (golden `families.SU`), and the BDX YAMLs sit in the same directory at
   the pin, so I expect a `BDX` family of 11 manual cases. If the image
   has none, `runFamily` fails "family BDX has no cases in this image's
   descriptor" and the row should go.
5. **The pinned image and the OTA tools (unverified).** matter.js HEAD's
   Dockerfile builds `chip-ota-provider-app` and copies
   `ota_image_tool.py`; whether the pinned digest
   (`ghcr.io/matter-js/chip@sha256:d6f1de89…`) already does is what the
   first SU run logs ("OTA plumbing: …").
6. **Image format vs brief N.** The image is built exactly as
   `build_ota_images.sh:106` does (sha256 digest, version string "2.0").
   N's report should confirm its requestor accepts that header.
7. **Descriptor argument values.** I did not see whether the descriptor
   keeps chip CI's `${OTA_PROVIDER_APP}` / `${SU_OTA_REQUESTOR_V2}`
   placeholders or substitutes them; `otaArgs` replaces the value after
   the prefix either way.
8. **The ESA's PowerAdjustment triggers.** Without PA the charger-shaped
   triggers that set PowerAdjustmentCapability log
   `attribute not served` on the ESA. What chip's app does with
   `--featureSet 0x7c` for those triggers is unverified; TC-DEM-2.6/2.8
   do not send them (their steps send Forecast Adjustment, Opt-out and
   Constraint triggers).
9. **The ESA's name** ("Charger ESA") is mine; chip's app has no second
   device.

## Entries for the owner

by_design (`notes/parity/by_design.md`):

- *BD-Matter-ElectricalEnergyTariffWithoutTagList* — the tariff endpoint
  omits the TAGLIST feature matter.js marks mandatory
  (`electrical-energy-tariff.element.ts:21`), same rationale as
  `BD-Matter-ClosureWithoutTagList` (`cluster/core/descriptor.go:177-186`).
- *Reference daemon: one DEM trigger, two ESAs* — chip's EVSE app has one
  DEM delegate; the daemon hands an endpoint-less 0x0098… trigger to both
  (`fleet_esa.go` `testEventTrigger`).

Findings register: none new.

## Fixture run the owner must dispatch

`chiptool.yml` dispatch with `update_fixtures=true` and
`families=IDM,DEM,EGC,SU,BDX`:

- **IDM** — a new bridged endpoint (the ESA): IDM/10.4 needs its golden
  entry (briefs README, lesson 1); the PICS slices are regenerated, the
  new endpoint gets its own.
- **DEM** — 2.6 and 2.8 become applicable on the ESA (golden
  `notApplicable` entries go, counts come).
- **EGC**, **BDX** — declared, never run.
- **SU** — first run of the OTA plumbing (the log line names the provider
  and image or what is missing).

Then `go test ./internal/chiptool/ -run TestCertifiabilityDocument -args -update-certifiability-doc`.
