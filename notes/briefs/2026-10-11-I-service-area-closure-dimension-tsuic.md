# Brief I — ServiceArea, ClosureDimension, ThermostatUserInterfaceConfiguration (phase 2b/2d)

Owner: Fable. Implementer: Opus. Branch `wave3/area-closure`, pushed, **no PR**.

## Sources (read, transcribe, cite file:line; derive nothing)

- `parity/schema.json` for ids and shapes: ServiceArea (0x0150),
  ClosureDimension (0x0105), ThermostatUserInterfaceConfiguration (0x0204).
- matter.js rules to port: `packages/node/src/behaviors/service-area/ServiceAreaServer.ts`
  (231 lines: SupportedAreas/SupportedMaps consistency, SelectAreas and
  SkipArea answers, CurrentArea/Progress reactors — port onto a
  hand-written server on `spec.Instance`, the `cluster/opstate` way),
  `thermostat-user-interface-configuration/ThermostatUserInterfaceConfigurationServer.ts`
  (16 lines). `closure-dimension/ClosureDimensionServer.ts` is empty: rules
  from chip at the pin 6170af84, `src/app/clusters/closure-dimension-server/`
  (SetTarget, Step; the limit and latching rules), and the test event
  triggers the CLDIM cases use (`closure-dimension` handler in the closure
  app); the daemon's ClosureControl in `cluster/closure` and
  `fleet_closure.go` is the pattern.
- Device types: RoboticVacuumCleaner 0x0074 (ServiceArea O; the daemon
  has one), ClosurePanel 0x0231 (ClosureDimension M; new device),
  Thermostat 0x0301 (ThermostatUserInterfaceConfiguration O; the daemon
  has one).
- Families: SEAR (5), CLDIM (11), TSUIC (2).

## Must
1. Definitions via clustergen for the three clusters.
2. `cluster/servicearea` (new): the ServiceArea server with the matter.js rules, host port for the device's area list, map list, current area and progress, and the commands' device-side decisions. `cluster/closure`: ClosureDimension server per chip with its triggers. `cluster/thermo`: ThermostatUserInterfaceConfiguration on `spec.Server` with the matter.js rule. Parity tests.
3. Reference daemon: ServiceArea on the vacuum (two areas, one map), a ClosurePanel (0x0231) device in `fleet_closure.go` with the CLDIM triggers, TSUIC on the thermostat.
4. Family rows SEAR, CLDIM, TSUIC, `epOf` entries, workflow group; CHANGELOG; floors; reachability.

Not in scope: PICS/golden; `cluster/energy`, `cluster/alarmbase`, `cluster/opstate` (brief G owns it; coordinate nothing — do not touch), `cluster/core`; daemon `wiring.go`/`main.go`/`fleet_appliances.go`/`fleet_energy.go`.

## Guards
As brief G.
