# Brief: issue #370 — EnergyReporting fake-readings test event trigger in the reference daemon

Branch `fix/370-energy-reporting-trigger` from `main` at the v0.16.0 tag. One
PR, title "reference-bridge: the EnergyReporting fake-readings test event
trigger; TC-EEM-2.2 and TC-EPM-2.2 run (#370)". CHANGELOG under
`[Unreleased]` → Added (the daemon's trigger) and Changed (the two cases
join the families). No release.

## Why

Issue #370. TC-EEM-2.2 and TC-EPM-2.2 are excluded from the chip-tool
families (`internal/chiptool/families_table_test.go:651-661`, class harness,
W6-test-03) because the daemon answers the EnergyReporting trigger with
INVALID_COMMAND. Owner decision 2026-11-02: build the trigger host-side.

## Sources (read these; cite them in the code)

- chip 6170af84 (`git -C ../connectedhomeip show 6170af84:<path>`):
  - `src/app/clusters/electrical-energy-measurement-server/EnergyReportingTestEventTriggerHandler.h:47-51`
    — the three trigger values: stop `0x0091000000000000`, load start 1 kW / 2 s
    `0x0091000000000001`, generator start 3 kW / 5 s `0x0091000000000002`.
  - `examples/energy-management/energy-management-triggers/EnergyReportingEventTriggers.cpp:38, :52, :58, :67-75`
    — the dispatch and the exact mW / mV / mA values and randomness each
    trigger passes to `StartFakeReadings`.
  - `examples/energy-management/energy-management-triggers/FakeReadings.cpp:73-175`
    — the model: on start (reset) `srand(1)`, totals 0; every `mInterval_s`
    seconds power = base ± randomness, voltage and current likewise;
    periodic energy = power × interval / 3600 (mWh), cumulative totals
    accumulate; the EPM attributes set are ActivePower, Voltage,
    ActiveCurrent; the EEM ones PeriodicEnergyImported/Exported and
    CumulativeEnergyImported/Exported (:151-168). Stop sets `bEnabled` false
    (:106) — the last readings stay.
  - `src/python_testing/TC_EPM_2_2.py:84-160` — the case: trigger start, wait
    3 s, ActivePower in 980000..1020000 mW, ActiveCurrent 3848..4848 mA
    (PICS EPM.S.A0005), Voltage 229000..231000 mV (EPM.S.A0004), wait 3 s,
    read again, trigger stop. `TC_EEM_2_2.py:85-124` — the same shape for
    the EEM attributes (read the exact checks).
- matter.js 862ad75f has no EnergyReporting trigger handler and no fake
  readings (`grep -rn "0x0091\|FakeReadings\|EnergyReporting" ../matter.js/packages --include=*.ts`
  — confirm, cite the absence as the Ahead marker: the behaviour follows
  chip).

## Where it goes (go-fabric)

- `examples/reference-bridge/control.go:409-425` — `fleet.testEventTrigger`,
  the flat dispatch table; add the electrical meter's handler to the chain
  the way `f.meter.testEventTrigger` and `f.esa.testEventTrigger` sit there.
- `examples/reference-bridge/fleet_energy_market.go:480-520, :571-600` —
  `demoElectricalMeter` (the grid meter, device type ElectricalMeter with an
  ElectricalSensor part; `electricalDeclarations()` at :621 declares EEM and
  EPM on both). Its `testEventTrigger` already handles the CommodityMetering
  triggers; the EnergyReporting triggers join it. Keep the pattern of the
  other demo devices: a goroutine or the node clock drives the interval,
  stop ends it, the device's `device(...)` wires the servers.
- The servers: `cluster/spec/electricalpowermeasurement/server_gen.go`
  (`Server.Set(Patch)`, :236) and
  `cluster/spec/electricalenergymeasurement/server_gen.go` (`Set(Patch)`,
  :132) — read the Patch types in the two `definition_gen.go` files for the
  attribute names (ActivePower, Voltage, ActiveCurrent; Cumulative/Periodic
  EnergyImported/Exported are EnergyMeasurementStructs with Energy and the
  time fields — read `cluster/measurement/` for how the daemon's other
  devices fill them, e.g. the EVSE in `fleet_evse.go`).
- Which endpoint the case reads: the families runner resolves the DUT
  endpoint by device type (`dtElectricalMeter`; read
  `internal/chiptool/families*.go` for the resolution). The fake readings
  must land on that endpoint; if the meter and its sensor part both carry
  EEM/EPM, update both (chip updates its one electrical-sensor endpoint 1).
- Use the daemon's clock seam if one exists (`grep -n clock examples/reference-bridge/*.go`)
  so the unit test does not sleep; the chip-tool case itself waits real
  seconds, which is fine.

## Deliverables

1. The handler: the three triggers; values and cadences from chip's lines
   above; deterministic (chip seeds `srand(1)`; use a fixed-seed
   `math/rand/v2` source or no randomness — say which in the comment and
   why the case's ranges still hold).
2. A unit test in `examples/reference-bridge/` (fleet_*_test.go pattern):
   start → the attributes read in the case's ranges; the energy accumulates
   over two ticks; stop → no further change.
3. `internal/chiptool/families_table_test.go`: remove the two `exclude`
   rows; `make chiptool-families-update` with `GOFABRIC_CHIP_FAMILIES=EEM,EPM`
   for the golden step counts (the harness is single-user: run it alone).
   Then `GOFABRIC_CHIP_FAMILIES=EEM,EPM make chiptool-families` green.
4. `notes/parity/by_design.md`, the *Ahead of matter.js* register (the table
   at the head of the file, "## Ahead of matter.js — register" line 34; read
   two existing rows for the shape): one row — marker: matter.js 862ad75f
   has no EnergyReporting trigger handler (cite the grep's absence by naming
   the directories searched); source: the chip files above; the entry
   paragraph.
5. CHANGELOG lines; the not-run table's text in `docs/` if a page lists the
   excluded cases (`grep -rn "EEM-2.2\|EPM-2.2" docs/ notes/ README.md`).
6. Guards: `make ci`, `make race` for `examples/reference-bridge` and
   `internal/chiptool`, `make ahead-check` (the new row counts),
   `make cite-check`, `make reachability-check` (regenerate if moved),
   `make apidiff-check`.

## Rules

- Author identity: `git var GIT_AUTHOR_IDENT` must print
  `SukramJ <sukramj@icloud.com>`; no Co-authored-by or other trailers.
- `export PATH=$HOME/go/bin:$PATH` before make; guard bundles under
  `flock ~/.cache/go-fabric-scratch/guards.lock`; logs under
  `~/.cache/go-fabric-scratch/i370/` with a summary (HEAD, one exit line per
  guard); read logs by grep.
- Read slices, not files. Cite chip paths with `at 6170af84`.
- Never commit anything from the CSA data model XML.
- Open the PR without auto-merge; report in the fixed shape (per
  deliverable: commit, test; guards with HEAD; differences from the brief;
  decisions; left out; open questions; by_design candidates).
