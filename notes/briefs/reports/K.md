# Report K — CommodityPrice, CommodityTariff, CommodityMetering, ElectricalGridConditions; a second ESA

Branch `wave4/commodity` (from `origin/main` 9426625), no PR. chip sources
were read at the harness pin
`git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`;
line numbers below are the pin's.

## What was built

| Commit | What |
| --- | --- |
| `spec: generated …` | `script/clustergen/clusters.go` lists the four clusters; `cluster/spec/{commodityprice,commoditytariff,commoditymetering,electricalgridconditions}` generated; floors 100 in `script/coverfloor/floors.go`. |
| `energy: …` | `cluster/energy/commodityprice.go`, `gridconditions.go`, `commoditymetering.go`, `commoditytariff.go` on `spec.Server`; tests `commodity_parity_matterjs_test.go`, `commodity_test.go`, `commodity_days_test.go`. |
| `reference-bridge: …` | `examples/reference-bridge/fleet_energy_market.go` (ElectricalEnergyTariff 0x0513, ElectricalMeter 0x0514 with an ElectricalSensor part), `control_energy_market.go` (`energyMarketTestEventTrigger`), `fleet_energy_market_test.go`; three lines in `fleet.go` (two fields, two constructors, the specs appended after the panel). |
| `chiptool: …` | `dtElectricalEnergyTariff`/`dtElectricalMeter`, rows `SETRF` and `COMMTR`, both in `epOf`; `app-energy=…,SETRF,COMMTR` in `chiptool.yml`. |
| `docs: CHANGELOG …` | `[Unreleased]` → Added. |

Each commit that changed Go code carries its regenerated reachability inventory.

Servers (rules transcribed, each cited in the code):

- **CommodityPrice** — `CommodityPriceCluster.cpp`/`.h`: TariffUnit kWh, Currency/CurrentPrice null at start (`.h:174-181`); ValidatePrice (`.cpp:91-112`); the attributes and the PriceChange event carry the price without description and components (`:296`, `:301`, `:463`), the detailed requests filter by Details and answer INVALID_COMMAND on an unknown bit (`:73-83`, `:337-379`); the two commands are opt-in (`OptionalCommandSet`, `.h:52-84`).
- **ElectricalGridConditions** — `electrical-grid-conditions-server.cpp`: carbon levels and period checks (`:117-136`), CurrentConditionsChanged on every set (`:142-163`), ForecastConditions FORE only.
- **CommodityMetering** — `commodity-metering-server.cpp`: all null at start (`:150-158`), MeteredQuantity needs a non-null maximum and ≤ maximum entries (`:192-203`), component ids cut to 128 (`:165-172`), maximum ≥ 1 (`:300-307`), TariffUnit enum (`:264-288`).
- **CommodityTariff** — everything in `commodity-tariff-server.cpp` and `CommodityTariffAttrsDataMgmt.cpp` except the storage scaffolding: per-attribute validation in chip's attribute order (`DataMgmt.cpp:403-1013`), the cross validation (`Server.cpp:138-288`), commit of changed attributes only (`DataMgmt.h:811-830`), delayed activation at StartDate (`Server.cpp:64-68`, `:316-328`), clear (`:330-357`), the derived Current/Next Day, DayEntry, DayEntryDate and TariffComponents (`:588-1091`), GetTariffComponent and GetDayEntry with FAILURE / NOT_FOUND (`:1099-1241`). Nothing of CommodityTariff is left out (the brief's fallback was not needed).

Reference daemon: the energy-gateway app's data — GBP/5 and kWh (`CommodityPriceDelegate.cpp:38,56-60`), local generation available (`ElectricalGridConditionsDelegate.cpp:41-46`), CommodityTariff features 63 (`CommodityTariffMain.cpp:73-76`, zap `:2240`), both tariff presets verbatim (`CommodityTariffSamples.h:29-415`), the metering samples (`CommodityMeteringEventTriggers.cpp:29-58`), the 30 s tariff poll (`CommodityTariffInstance.cpp:66-77`, `.h:27`), the TestTimeManager as a clock of the tariff server (`CommodityTariffEventTriggers.cpp:42-195`), and all eleven triggers of the four clusters (price 0x0095…0/1, grid 0x00A0…0/1, tariff 0x0700…0-4, metering 0x0b07…0/1), endpoint bits masked as chip's `clearEndpointInEventTrigger`.

## Guards

| Guard | Result |
| --- | --- |
| `go test ./cluster/... ./examples/... ./schema/... ./script/...` | green |
| `go test ./internal/chiptool/ -run 'TestWorkflow\|TestFamil'` | green; `go vet -tags chiptool ./internal/chiptool/` clean |
| `make lint` | 0 issues (both runs), `go mod tidy -diff` and `verify` clean |
| `make cover-check` | red only on the two expected failures: `TestCertifiabilityDocument` (SETRF and COMMTR "declared but has never run") and the darwin mdns `TestBuildOperationalService_EmptyHostName_UsesDefault`. Measured: `cluster/energy` 94.7 % (floor 93), the four generated packages 100 %. |
| `make reachability-check` | green |
| race: `cluster/energy`, `examples/reference-bridge`, the four spec packages | green |

What proves what: `TestParityMatterJS_CommodityServers` (every feature selection against the matter.js conformance via `spectest.CheckServer`, device-type membership); `TestCommodityPrice`, `TestElectricalGridConditions`, `TestCommodityMetering` (each chip rule); `TestCommodityTariffRules` (one mutation per validation and cross-validation rule, refusal leaves the tariff untouched); `TestCommodityTariffDerived` (TC-SETRF-2.3's 4 h / 24 h sequence and the commands on Sample 2); `TestCommodityTariffDays` (individual day, calendar day, entries without duration, label joining and its 128 cut); `TestCommodityTariffDelayed`; daemon `TestTariffTriggers` (SETRF-2.2/2.3 trigger sequence on both presets), `TestMeteringTriggers` (TC-COMMTR-3.1: every attribute changes after the first update), `TestPriceAndGridTriggers`, `TestEnergyMarketDevices` (clusters, features, the device-type validator).

## Left out, and why

1. **The second ESA (Must 4) is not in the daemon.** `energy.NewDeviceEnergyManagement` with SFR|STA|PAU|FA|CON (0x7C) fails: `spec: illegal feature selection: DeviceEnergyManagement feature PowerForecastReporting is mandatory`. Cause, measured:
   - PFR's conformance is `[!PA].a, STA | PAU | FA | CON, O` (`cluster/spec/deviceenergymanagement/definition_gen.go:889`).
   - `spec.CheckFeatures` (`cluster/spec/instance.go:98-126`) judges each feature with `Conformance.Applicability` (`cluster/spec/conformance.go:122-151`), which mirrors matter.js's element `computeApplicability` (`packages/model/src/aspects/Conformance.ts:913-958`): an Optional entry of an otherwise list does not end the list, a later Mandatory entry wins — so PFR is mandatory whenever STA|PAU|FA|CON is selected.
   - matter.js judges a *feature selection* differently: `FeatureSelectionErrors` (`packages/model/src/logic/cluster-variance/FeatureSelectionErrors.ts`) uses `IllegalFeatureCombinations`, where "the first applicable entry governs" (`IllegalFeatureCombinations.ts:389-425`, `inapplicable` `:282-305`): with !PA the `[!PA].a` entry governs and PFR is optional. matter.js HEAD therefore accepts 0x7C; chip runs it (`TC_DEM_2_6.py:30`, `TC_DEM_2_8.py:30`: `--featureSet 0x7c`).
   - So `spec.CheckFeatures` diverges from the matter.js function its comment cites. The fix is in `cluster/spec` (brief L's area, not this brief's). A workaround in the daemon would have meant bypassing the check. The ESA is ready to wire once `CheckFeatures` follows `IllegalFeatureCombinations`. It is the charger's `demoDem` with `Features: SFR|STA|PAU|FA|CON` and the same delegate, modes and triggers, as a standalone bridged device stating `ControllableEsa`. The draft is about 70 lines (a `newDemoEsa` beside `newDemoDem`, plus an `esaSpec`).
   - Trigger routing for it: the DEM trigger codes carry no endpoint, and `control.go`'s dispatch stops at the first handler (`f.evse.testEventTrigger`). The draft passed a DEM trigger to the second ESA, returned false, and let the dispatch go on to the charger, so both ESAs answer it.
   - `epOf`: `br.endpointFor(t, dtDeviceEnergyManagement)` resolves to the *first* 0x050D endpoint, the charger's part. Proposal: a pseudo key like `dtOvenCavity` (`familyrun_test.go:162`), e.g. `dtStateForecastEsa = dtDeviceEnergyManagement<<16 | dtDeviceEnergyManagement`, resolved with `br.endpointAfter(t, dtElectricalMeter, dtDeviceEnergyManagement)` (the ESA's spec comes after the meter's in the snapshot). The DEM family would then need a way to run 2.6 and 2.8 on that key. That is the owner's decision about the table.
2. **The `control.go` dispatch line** (owner): append `|| f.energyMarketTestEventTrigger(trigger)` to the `if` at `control.go:375-377`. Until then the daemon does not answer the eleven triggers (the tests call the method directly).
3. **PICS, golden, fixtures** (owner): two new bridged endpoints plus the meter's ElectricalSensor part, so the fixture run needs **IDM** next to SETRF and COMMTR (README lesson 1).

## Open questions

1. **Descriptor TAGLIST on ElectricalEnergyTariff.** matter.js requires it (`electrical-energy-tariff.element.ts:21`, `ElectricalEnergyTariffRequirements.DescriptorServer = BaseDescriptorServer.with("TagList")`). This module's Descriptor does not serve TagList (`cluster/core/descriptor.go:94`). `endpoint.ValidateDeviceTypes` reports `missing ElectricalEnergyTariff Descriptor.TAGLIST`; `TestEnergyMarketDevices` tolerates exactly that one finding and fails on any other. Same class as BD-Matter-ClosureWithoutTagList? Which tags the energy tariff would carry (the commodity-tariff-{commodity,flow,chronology} namespaces) is not in the brief.
2. **The brief's paths differ from the pin.** The demo data and triggers are in `examples/energy-gateway-app/` (`commodity-price/`, `commodity-tariff/`, `commodity-metering/`, `electrical-grid-conditions/`), not in `examples/energy-management-app/energy-management-common/`. `TC_EGC_2_1/2_2/2_3.py` (PICS `EGC.S`, triggers 0x00A0…0/1, `TC_EGCTestBase.py:36-37`) and `TC_SETRF_3_1.py` exist at the pin; the brief says there are none for ElectricalGridConditions. The daemon already answers the EGC triggers. Should an `EGC` row go on `dtElectricalEnergyTariff`?
3. **ClusterRevision of CommodityTariff:** the snapshot says 1 (`definition_gen.go:25`), the app's zap says 2 (`energy-gateway-app.matter:2241`). The server serves the snapshot's value; `make chipdm-check` was not run.
4. **LocalGenerationAvailable is writable** (RW VO in the definition) and served writable, as matter.js would. chip's AAI has no `Write` and reads its own member (`electrical-grid-conditions-server.cpp:60-90`), so a write there probably never shows. Unverified what chip answers to the write.
5. **The tariff's test clock is scoped to the CommodityTariff server.** chip's MockClock replaces the whole app's clock (`CommodityTariffEventTriggers.cpp:103`), which also moves the timestamps the price, grid and metering triggers take. Accept as by-design?
6. **Choices where chip is undefined or crashes:**
   - An update without DayPatterns. chip calls `.Value()` on a null new value (`Server.cpp:156-157`); here it is an empty set.
   - `ClearTariff` drops a pending delayed update. chip's `Cleanup` leaves the objects validated (`DataMgmt.h:836-848`).
   - A null new value for a scalar attribute cannot be expressed. The app never sends one (`CommodityTariffEventTriggers.cpp:215-231`).

## Entries for the owner

by_design (`notes/parity/by_design.md`):

- *CommodityMetering refused list keeps the old value.* chip frees the stored list before it validates the new one (`commodity-metering-server.cpp:183-203`); go-fabric leaves the attribute as it was.
- *CommodityTariff derived attributes report their reset.* chip nulls Current*/Next* without a report (`Server.cpp:575-586`); `spec.Server` reports every change.
- *CommodityPrice/EGC setters* emit the event on every set, as chip does (`CommodityPriceCluster.cpp:434`, `electrical-grid-conditions-server.cpp:143`), but report the attribute only on a change.
- *Tariff test clock scoped to the server* (question 5), *DayPatterns absent* and *Clear drops a pending update* (question 6), if accepted.
- *Equal PeriodStart/PeriodEnd:* chip's setters allow it (`CommodityPriceCluster.cpp:93`, `electrical-grid-conditions-server.cpp:132`); the definition's `min periodStart + 1` is an expression bound `spec` does not evaluate (`cluster/spec/write.go:334`), so the setters follow chip.

Findings register (`notes/parity/matter_behaviour_findings.md`):

- **`spec.CheckFeatures` vs matter.js `FeatureSelectionErrors`** (Left out 1): an otherwise list's first applicable entry governs in matter.js and does not in go-fabric. DEM 0x7C is refused, so TC-DEM-2.6/2.8 cannot get an ESA.

chip behaviours ported verbatim that the CI run may surface (not defects here, recorded for classification):

- NextDayEntryDate is CurrentDayEntryDate plus the minutes the current entry has left *from now* (`Server.cpp:1067`). Off a boundary, that is early by the minutes already elapsed.
- With no covering entry, the last walked entry becomes current (`Server.cpp:749-797`).
- A calendar period is found only on its start day, or as the StartDate-0 fallback (`Server.cpp:693-710`).
- A day entry may not appear in two day patterns, nor in a pattern and an individual day (shared `RefsToDayEntryIDsFromDays`, `DataMgmt.cpp:412`, `:916`).
- Thresholds conflict only within one period (`Server.cpp:259-284`).
