# Brief O — harness follow-ups of wave 4 and the SU / BDX family plumbing

Owner: Fable. Implementer: Opus. Branch `wave5/harness`, pushed, **no PR**.

## Sources (read, transcribe, cite file:line; derive nothing)

- `internal/chiptool/families_table_test.go` (family rows, `exclude`,
  `edits`, `args`, `picsEdits`), `familyrun_test.go` (`epOf` :160-162 with
  the `dtOvenCavity` pseudo key resolved by `endpointAfter`; the case loop
  :349 picks `pics.endpoint(t, dt)` and `pics.byEP[ep]` from the row's
  one device type; `applyEdits` :377; `runPythonCase` :403), `chipcases_test.go`,
  `testdata/reference-bridge.pics` (:16-17 forbids declaring a code the
  device-generated slices carry), `docs/certifiability.md`.
- Second ESA: `notes/briefs/reports/K.md` "Left out 1" has the design
  (~70 lines: a `newDemoEsa` beside `newDemoDem` with features
  STA|PAU|FA|CON|SFR = 0x7C, same delegate, modes and triggers; the DEM
  trigger codes carry no endpoint, so both ESAs answer and the dispatch
  must not stop at the first handler). `spec.CheckFeatures` accepts 0x7C
  since #47 (`cluster/spec/instance.go` `featureApplicability`). DEM/2.6
  needs `DEM.S.F05 & F02`, DEM/2.8 `F06 & F02` (`TC_DEM_2_6.py:30`,
  `TC_DEM_2_8.py:30`: `--featureSet 0x7c` at the pin); DEM/2.9 and 2.11
  stay (b) (reasons in `BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer`).
- Wire-shape defect class: `notes/briefs/reports/K.md` and the #47 fix-up
  (`cluster/energy/commodity_wire.go`, `TestEnergyMarketWireShapes` in
  `examples/reference-bridge`): a `MatterRead` value that is a plain Go
  slice of generated structs falls to the bridge writer's null default
  (`bridge/reply.go` `defaultAttributeValueWriter`). Candidates the fix-up
  named: Descriptor DeviceTypeList, OperationalStateList, ModeBase
  SupportedModes, `measurement.AccuracyStruct`.
- EGC: `TC_EGC_2_1.py`, `TC_EGC_2_2.py`, `TC_EGC_2_3.py` exist at the pin
  (ElectricalGridConditions on the tariff device, `fleet_energy_market.go`).
- TagList: matter.js requires the Descriptor TAGLIST feature on
  ElectricalEnergyTariff (`electrical-energy-tariff.element.ts` — cite the
  line); `cluster/core/descriptor.go` (~:94, :177-186, :231-237) holds the
  module's TagList code; `BD-Matter-ClosureWithoutTagList` records the
  closure endpoint's omission. Decide from the code whether TagList can be
  served on the tariff and meter endpoints; if yes do it with matter.js's
  semantic tags for the device types (cite), else record the entry.
- CommodityTariff ClusterRevision: snapshot 1, the energy-gateway-app zap
  says 2 (report K, question 4): run `make chipdm-check` and read
  `docs/chip-datamodel-crosscheck.md`; classify only what the cross-check
  reports (never acknowledge a difference to make it pass).
- SU and BDX families: matter.js's chip-testing runs them with chip's
  `chip-ota-provider-app` from the image and its own requestor as the DUT
  (`../matter.js/support/chip-testing/test/cert/AGENTS.md:61`,
  `src/OtaRequestorTestInstance.ts`, `test/cert/tc-su-support.ts`,
  `tc-bdx-support.ts` — read how they build the OTA image with
  `ota_image_tool.py`, start the provider, and pass
  `provider_app_path`, `ota_image`, `ota_provider_port` to the Python cases;
  `TC_SU_2_2.py:10-27` at the pin shows the arguments the case takes and
  that the case itself starts the provider app). Our daemon becomes the
  requestor DUT when brief N lands; until then the family rows are added
  with the cases' gates honest (the requestor cluster absent → PICS false).
- `deprecation_policy_test.go`: brief L's report says it does not check a
  deprecated `const (` group (the five ModeSelect constants).

## Must
1. Per-case endpoint override in the family table: `endpoints map[string]uint32`
   (case → device type or pseudo key); for such a case the harness takes the
   endpoint **and its PICS slice** from that key (`familyrun_test.go:349-363`).
   Pseudo key `dtStateForecastEsa` resolved with `endpointAfter`. DEM/2.6
   and 2.8 run on the second ESA.
2. The second ESA in the daemon (new file `fleet_esa.go`, per report K's
   design; the DEM trigger dispatch answers both ESAs). `epOf` and the
   workflow group unchanged for DEM.
3. Fleet-wide wire-shape guard: a test in `examples/reference-bridge` that
   walks every endpoint of the fleet, reads every attribute of every server
   through the bridge's writer path, and fails on a value the writer would
   encode as null while the definition says it is not nullable, plus the
   plain-slice check; fix what it finds in the servers (as `spec.List`), one
   commit per package, each citing the guard.
4. EGC family row (device type dtElectricalEnergyTariff; group `app-energy`).
5. TagList on ElectricalEnergyTariff / ElectricalMeter, or the classified
   entry for the owner.
6. `make chipdm-check`; if CommodityTariff's revision differs, classify per
   `internal/chipdm/acknowledged.go`'s classes with the evidence.
7. SU and BDX family rows and the plumbing: the image built in the
   container with `ota_image_tool.py` for the daemon's vendor/product id and
   software version + 1 (coordinate the format with brief N's report; until
   N lands, build it anyway so the plumbing is testable), the provider
   binary path found in the container (`which chip-ota-provider-app`), the
   case arguments as the Python cases' headers name them, `PIXIT`s per
   case. Do not declare PICS the device-generated slices will carry.
8. `deprecation_policy_test.go` checks `Deprecated:` paragraphs inside
   `const (` groups too (the ModeSelect constants become its first case).
9. CHANGELOG; `make reachability` after every commit.

Not in scope: `cluster/spec` runtime, `secure/`, `bridge/` internals (brief M
owns `bridge/` this wave — if the wire-shape guard needs a bridge helper,
ask for it in the report and keep the guard in `examples/`), `bdx/` and
the requestor (brief N), `fleet_evse.go` (copy the pattern into `fleet_esa.go`).

## Guards
`go test ./internal/chiptool/ ./examples/... ./cluster/... ./schema/...`,
`go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'`, `make lint`
(gofumpt v0.12.0), `make cover-check`, `make reachability`, `make chipdm-check`.
`TestCertifiabilityDocument` red until the owner's fixture run. Report in
`notes/briefs/reports/O.md` as brief M describes.
