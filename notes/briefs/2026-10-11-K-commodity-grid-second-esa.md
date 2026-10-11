# Brief K — CommodityPrice, CommodityTariff, CommodityMetering, ElectricalGridConditions; a second ESA (phase 2c rest)

Owner: Fable. Implementer: Opus. Branch `wave4/commodity`, pushed, **no PR**.

## Sources (read, transcribe, cite file:line; derive nothing)

- Cluster ids and shapes: `parity/schema.json` only; definitions via
  `script/clustergen` (add CommodityPrice, CommodityTariff, CommodityMetering,
  ElectricalGridConditions to `script/clustergen/clusters.go`).
- matter.js servers `commodity-price/CommodityPriceServer.ts` (14 lines),
  `commodity-tariff/CommodityTariffServer.ts` (17), `commodity-metering/
  CommodityMeteringServer.ts` (14), `electrical-grid-conditions/
  ElectricalGridConditionsServer.ts` (14) add no rules. Device types:
  `packages/node/src/devices/electrical-energy-tariff.ts`,
  `electrical-meter.ts`.
- Rules come from connectedhomeip at the harness pin 6170af84, read with
  `git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`
  (the local checkout is at another commit; cite the pin's lines):
  - `src/app/clusters/commodity-price-server/CommodityPriceCluster.cpp` (474)
  - `src/app/clusters/electrical-grid-conditions-server/electrical-grid-conditions-server.cpp` (179)
  - `src/app/clusters/commodity-metering-server/commodity-metering-server.cpp` (374)
  - `src/app/clusters/commodity-tariff-server/commodity-tariff-server.cpp` (1278)
    and `CommodityTariffAttrsDataMgmt.cpp` (1107) — the largest; port the
    attribute validation and the GetTariffComponent / GetDayEntry command
    rules, not the C++ data-management scaffolding.
  - the energy-management app's delegates for demo data and test event
    triggers (`examples/energy-management-app/energy-management-common/`).
- Device types (`schema/devicetypes.go`): ElectricalEnergyTariff 0x0513
  (Descriptor; CommodityPrice `[ActiveTariff].a+`, ElectricalGridConditions
  O, CommodityTariff `O.a+`; :803-808) and ElectricalMeter 0x0514
  (ElectricalPowerMeasurement M, ElectricalEnergyMeasurement M,
  CommodityPrice, ElectricalGridConditions, CommodityTariff, CommodityMetering
  `P, M`; :809-817). The daemon's EV charger already carries an
  ElectricalSensor part with EPM/EEM (`fleet_evse.go`, brief H) — reuse
  those constructors.
- Families in chip at the pin: SETRF (`TC_SETRF_2_1/2_2/2_3.py`, cluster
  CommodityTariff, header `--endpoint 1`, ci-pics `SETRF.S=1`), COMMTR
  (`TC_COMMTR_2_1.py`, `TC_COMMTR_3_1.py`, `TC_COMMTR_TestBase.py`, PICS
  `COMMTR.S`). None for CommodityPrice or ElectricalGridConditions.
- DEM second ESA: the daemon's DeviceEnergyManagement part has
  PA|PFR|STA|PAU|FA|CON (PICS `DEM.S.F00..F06`, ep of dtDeviceEnergyManagement).
  SFR (bit 2) conforms `[!PA].a` and PFR `[!PA].a, STA|PAU|FA|CON, O`
  (`cluster/spec/deviceenergymanagement/definition_gen.go:888-894`): with PA
  absent exactly one of PFR / SFR. DEM/2.6 needs `F05 & F02`, DEM/2.8
  `F06 & F02` — reachable by a second ESA with STA|PAU|FA|CON|SFR; DEM/2.9
  needs `F01 & F02` (PFR and SFR together), which the conformance forbids —
  stays (b). DEM/2.11 needs `F07` PowerRangeAdjustment, absent from the
  matter.js snapshot and the 1.6.1 data model — stays (b). Read
  `TC_DEM_2_6.py`/`TC_DEM_2_8.py` at the pin for the triggers they send.

## Must
1. Definitions via clustergen for the four clusters; floors 100.
2. `cluster/energy`: `NewCommodityPrice`, `NewElectricalGridConditions`,
   `NewCommodityMetering`, `NewCommodityTariff` on `spec.Server` with chip's
   rules and host ports for the data (price forecasts, grid conditions,
   metered quantities, tariff components / periods / day entries); the
   GetDetailedPriceRequest / GetDetailedForecastRequest /
   GetTariffComponent / GetDayEntry commands and their statuses per chip,
   each cited; events (PriceChange, CurrentConditionsChanged, …) through
   `contract.EventEmitter`.
3. Reference daemon: an ElectricalEnergyTariff (0x0513) device and an
   ElectricalMeter (0x0514) device in a new `fleet_energy_market.go`, with
   demo data the SETRF and COMMTR cases accept (read the cases at the pin
   for what they read and write), and the test event triggers those cases
   send (add them through a new `control_energy_market.go`; ask the owner
   to merge the one-line dispatch in `control.go`).
4. A second ESA in the daemon (new file `fleet_esa.go`): a
   DeviceEnergyManagement (0x050D) device with features STA|PAU|FA|CON|SFR
   and DeviceEnergyManagementMode, answering the DEM triggers as the first
   one does (`fleet_evse.go` is brief H's — copy the pattern into the new
   file, do not edit it). Document in the report that `epOf` resolves
   dtDeviceEnergyManagement to the *first* such endpoint, and propose how the
   owner should address the second (a pseudo key like `dtOvenCavity`,
   `familyrun_test.go:162`).
5. Family rows SETRF and COMMTR (device types dtElectricalEnergyTariff,
   dtElectricalMeter in `epOf`), workflow group `app-energy`; CHANGELOG;
   floors; `make reachability` after **every** commit.

Not in scope: PICS/golden (owner); `cluster/spec` runtime files (brief L);
`fleet_evse.go`, `control.go`, `fleet_kitchen.go`, `fleet_sensors.go`
(briefs H/J own them); `secure/`, `im/`, `endpoint/`, `bridge/`.
TemperatureAlarm's SetTemperatureAlarmThresholds stays refused: no source
at the pin carries a rule (no `temperature-alarm-server` directory, matter.js
17-line server).

## Guards
As brief J. `TestCertifiabilityDocument` red until the owner's fixture run.
Report in `notes/briefs/reports/K.md` as brief J describes.
