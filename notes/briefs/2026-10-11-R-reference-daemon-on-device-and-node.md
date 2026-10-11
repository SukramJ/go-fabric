# Brief R — the reference daemon on `device/` and `node/` (phase 4, ADR 0018)

Owner: Fable. Implementer: Opus. Branch `wave6/daemon` from the owner's
integration branch `wave/2026-10-11-tranche-6` (P and Q merged), pushed,
**no PR**. Report in `notes/briefs/reports/R.md`. Starts when the owner
says P and Q are integrated.

Read first: ADR 0018 (decision 6), `notes/briefs/reports/P.md` and `Q.md`
(the surfaces as built), `examples/reference-bridge/README.md`,
`internal/chiptool/doc.go` (the local chip-tool run).

## Sources
- `device/` and `node/` as merged; `endpoint/spec.go`.
- The daemon: `main.go` (bring-up, :84-497), `wiring.go`, `commissioning.go`,
  `persist.go`, `fleet*.go` (every device: its `endpoint.Spec` literal in
  `fleet.go:1576-1648` and `surfaceSpecs` :1655-1664, its
  `contract.EndpointSource` implementation and its cluster servers; parts
  at `fleet.go:1679-1683` and `fleet_appliances.go:710-718`), `control.go`
  (app pipe and test event triggers — unchanged behaviour), `ota.go`.
- `internal/chiptool/families_table_test.go` (`endpointDeviceTypes`,
  `pseudoEndpoints`, `epOf`: the harness finds endpoints by device type;
  the endpoint **numbers and order must not change**, the PICS slices
  `internal/chiptool/testdata/pics/ep*.txt` and the golden are per
  endpoint), `internal/chiptool/doc.go` (local run: Docker + avahi).

## Must
1. `main.go` becomes `node.New(cfg)` + `Aggregator().Add(...)` per device
   + `Run`. Every flag keeps its name and meaning (`--db`, `--listen`,
   `--advertise`, `--tcp`, `--ota-dir`, `--ota-harness`, `--app-pipe`, the
   discriminator / passcode / PBKDF flags; read `main.go:84-122`). What
   the facade does not cover (the app pipe, the test event triggers, the
   OTA applier, `printPairingInfo`, `printTopology`) stays in the daemon
   on the facade's accessors.
2. Every fleet device becomes a `device.New<Type>(opts, …)` call. A device
   whose cluster rules live in a module server with a host port
   (LevelControl, ColorControl, WindowCovering, DoorLock, Thermostat, Fan,
   Pump, Valve, ModeSelect, ModeBase modes, OperationalState, Closure,
   DEM, EVSE, WaterHeater, the appliance controls, …) keeps that server
   through `device.WithServer(...)`; a device whose server was the
   daemon's own `spec.Server` or a hand-written stateless one (the
   `onOffServer`, the sensor servers over `contract.MeasurementSource`?
   — check what `device/` provides for measurement types and keep the
   measurement sources where `endpoint.Spec.Measurement` is the path) uses
   the layer's default and the host hooks (`OnChanged`, `Set`). Parts
   through `AddPart`. The hand-written `onOffServer` in `fleet.go` goes;
   `cluster/onoff.Server` replaces it (lighting state, StartUpOnOff).
   `contract.EndpointSource` no longer appears in the daemon
   (`grep -rn EndpointSource examples/reference-bridge` is empty).
3. **Endpoint ids, device types, cluster sets, attribute values, PICS and
   app-pipe behaviour are unchanged**: the wire-shape guard
   (`TestFleetWireShapes`), `conformance_test.go`, `wiring_test.go`,
   `fleet_test.go` and the other daemon tests pass unchanged or with the
   smallest edit the new construction needs (say which and why in the
   report). The PICS slices under `internal/chiptool/testdata/pics/` must
   regenerate identical: run `make chiptool-families GOFABRIC_CHIP_FAMILIES=IDM`
   locally (Docker, avahi; the harness writes the slices and fails on a
   difference) and `make chiptool-test`. Both are local in this wave;
   there is no CI dispatch. Attach the family log summary to the report.
4. `examples/reference-bridge/README.md` and the package doc describe the
   new shape; CHANGELOG → Changed (the daemon is not API, but the example
   is what README points a host at).
5. Reachability after every commit; `make lint`; `make cover-check`
   (daemon floors), `make race` on `examples/reference-bridge`.

Not in scope: changes to `device/` or `node/` beyond a bug found while
migrating — fix it in the same branch with a test and name it in the
report, or stop and ask; `internal/chiptool` tables (unchanged if the
endpoints are unchanged); by_design / findings (list for the owner).

## Guards
`make ci`, `make chiptool-test`, `make chiptool-families GOFABRIC_CHIP_FAMILIES=IDM,DESC,BRBINFO,ACE`
(the families that read the topology; extend if a device's family is
cheap). Report: what was built, which guard proves it, what was left out
and why, every open question, every test edit with its reason.
