# Report BV-0 — v0.10.1: the six findings of loom's migration (#220–#225)

Branch `m4/bv0` from `origin/main` at `v0.10.0` (12cedc28). Brief
`notes/briefs/2026-10-22-BV0-v0.10.1-loom-findings.md`. matter.js pin
`862ad75f`. No composition or FeatureMap moves, no chip-tool run.

## §1 #225 — the 32-bit build

`node/credentials.go`: `WindowCredentials` clamps through `int64`, no
`nolint`. Guard: CI job `build (32-bit)` (`.github/workflows/ci.yml`) runs
`make build-32bit` (new, in `make help` and in `make ci`):
`CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -gcflags=-e ./...`
and `GOOS=linux GOARCH=386 go vet ./...`. Both print nothing; so does
`go build -gcflags=-e ./...` for linux/386. CHANGELOG `### Fixed`.

## §2 #222 — `NativeMover` on the generated request

`levelcontrol.NativeMover.MoveLevel(ctx, req lvldef.MoveRequest, withOnOff)`;
the source converts with the existing `moveOf`. The grep the brief names,
widened to interface methods, aliased imports (`clusterwire`) and struct
fields, finds no other exported port over a `cluster/wire` request. The
`wire` level and fan-step requests stay: the engine's internal `source`
interface and `fan/port.go` still use them. `TestNativeSourceMover` asserts
the generated request reaches the port. CHANGELOG `### Changed`, guide row.

## §3 #220 — `device.HostServer(srv)`

`device.HostServer(srv contract.ClusterServer) Declaration`
(`device/options.go`) resolves `srv.MatterClusterID()` through
`hostServers`, which `script/devicegen` writes (`script/devicegen/hostserver.go`
→ `device/hostserver_gen.go`, 119 entries, one per committed
`cluster/spec` package; the generator fails when a package has no
snapshot cluster). A cluster without a package goes to `WithServer`'s
mount. `go doc ./device HostServer` shows the one route and when to prefer
the typed `Declare`. Tests (`device/hostserver_test.go`): loom's shape (ten
servers on a RootNode, three without a package, each mounted), the
resolved type, declared twice, the manufacturer-specific path, nil, and
the table against `spec.Lookup`. CHANGELOG `### Added`, guide row, the
device page's options table.

## §4 #221 — `device.Measurement(src)`

`device/measurement.go`. The kit hands the source to the assembler
(`endpoint.Spec.Measurement`; a battery-class source goes to
`Spec.PowerSource`). The assembler runs `measurement.FromMeasurementClass`
with the endpoint number at assembly (`endpoint/materialize.go:81-91`).
At `New` the materialiser runs once with no endpoint, only to learn the
cluster set. The kit mounts no default server for those clusters and
refuses every other declaration of them. This is the v0.9.1
`WithMeasurement` mechanism, as a declaration. Test on `fabrictest`
(`device/measurement_fabric_test.go`): all 21 built-in classes that have a
materialiser, each on its class's device type, read over the wire and
compared with what the materialiser serves. The momentary switch is
included. The battery rides on a TemperatureSensor, and its EndpointList
names the device's endpoint. `endpoint.ValidateDeviceTypes` reports
nothing on the bridged endpoints. Refusals tested. Guide row (the new
form; what a host on `endpoint.Spec.Measurement` loses), advanced seams,
the device page. CHANGELOG `### Added`.

## §5 #223 — the validator's route (docs)

Guide rows for `secure/setup` and `secure/spake2`, and an
advanced-seams `secure/…` paragraph: a validator without a node imports
`secure/setup` (`IsValidSetupPIN`, `MaxDiscriminator`) and
`secure/spake2` (`IterationsMin/Max`, `PBKDFMin/MaxSaltSize`). Their
in-module dependencies are only `secure/sessionparams` and `tlv`
(`go list -deps`), against 191 for `node`. The measured 18.4 MB →
32.4 MB is the reason given.

## §6 #224 — the guide's three gaps (docs)

DoorLock: a "Changed" row and item 4 of "What a migrating host meets"
(`lock.FeatureUBOLT`; `lock.Feature` is `doorlockdef.Feature`, so
`doorlockdef.Declare{Features: lock.FeatureUBOLT}` on the kit). Store:
item 5 and the advanced-seams `store` row name `matter_thermostat_presets`
(schema v5, first tag `v0.8.3`) and `matter_doorlock_users`,
`_credentials`, `_schedules` (v6, first tag `v0.8.4`), from
`store/migrations/v5.up.sql`, `v6.up.sql`. Pin: the intro says the table
was checked against loom's `v0.6.0` pin and that loom migrated from
`v0.7.3`, where the cited lines held (per the issue).

## Guards

| Guard | HEAD | Exit |
| --- | --- | --- |
| `make ci` | see PR | see PR |
| `make race` | 6bf4cc6a | 0 |
| `make reachability` → `-check` | 6bf4cc6a | 0 (unreached 5, unchanged) |
| `make cite-check` | e60c9a6c | regenerated (line shifts, one new citation, drift unchanged) |
| `make testparity-check` | 6bf4cc6a | 0 |
| `go test ./script/...` | 6bf4cc6a | 0 |
| `mkdocs build --strict` | fefd58e6 | 0 |

## Differences from the brief

- `secure/setup`'s check is `IsValidSetupPIN`; `ValidateSetupPIN` exists
  only in `node`.
- `hostServers` maps to `func(contract.ClusterServer) Declaration` (the
  generated `Declare` value), not to `spec.ClusterDeclaration`. Only the
  value goes through the kit's typed path, which records the cluster as
  declared and refuses a second declaration. It is written by
  `script/devicegen`, which already knows the committed packages.
- #221: the kit does not build the servers itself at add time. The
  assembler builds them at assembly, the first moment the endpoint number
  exists. The bridge's measurement wiring keys on `Spec.Measurement`
  (`bridge/subscribe.go:1213-1214` notifier, `bridge/bridge.go:1034-1046`
  switch presses), and a copy the kit mounted would lose it. Named
  `Measurement`, matching `HostServer`; `WithMeasurement` stays gone.
- #224: the battery source is a second `Measurement` call (to
  `Spec.PowerSource`), because `contract` gives the battery no device
  type of its own.
- One CHANGELOG line under `### Changed` covers the two docs issues.

## Left out

- `closure.ControlServer`'s exported setters (`SetCurrentPosition`,
  `SetMainState`, `SetErrorList`, `ReportError`) take `cluster/wire`
  types. They are server methods, not ports.
- `opstate`'s `*clusterwire.ElapsedS` config fields and
  `node.RootServers.AdministratorCommissioning *wire.…`: not request
  types.

## Questions

1. Should the closure setters (above) move to `closuredef` types as their
   own finding?
2. Should `build (32-bit)` become a required check?

## `by_design` candidates

None. `Measurement` restores the v0.9.1 projection, which has no
matter.js counterpart beyond the Datasource citation in its doc.
