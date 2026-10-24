# Report CE — M4 phase 22: embed in a daemon, the operations page

Brief: `notes/briefs/2026-10-24-CE-m4-phase-22-embed-in-a-daemon.md`.
go-fabric branch `m4/phase-22-ce`; daemon branch `fabric-projection`.

## Daemon PR

[SukramJ/go-zendure2mqtt#64](https://github.com/SukramJ/go-zendure2mqtt/pull/64), no auto-merge. It pins `github.com/SukramJ/go-fabric v0.10.8`. The Matter side is off by default.

| Recipe step | Commit |
| --- | --- |
| 1 dependency | `6de3e76` (with 2) |
| 2 `node.Config` from the config | `3cfa56c` (the `MATTER_*` block), `6de3e76` (`fabric.NodeConfig`, `fabric.New`) |
| 3 device | `0bac952` (`newUnit`, `battery.go`) |
| 4 aggregator batch | `0bac952` (`addKnown`, `ensure`, `markComplete`) |
| 5 readings → attributes | `3c05e46` |
| 6 lifecycle, reachability | `c687e7e` (`coordinator.Projection`, `Deps.Fabric`, main wiring) |
| 7 pairing code | `f603c28` (log line, `<name>/info`, web UI `/api/health`) |
| 8 docs | `1d552bf` (README "Matter controllers", config rows, changelog, CLAUDE.md) |
| — | `180fab4` corrects the go-licenses note in the Makefile (pushed before the coordinator asked for no further pushes; nothing the guide cites moved) |

**Config block** (`internal/config`): `MATTER_ENABLED` (false), `MATTER_LISTEN` (`:5540`), `MATTER_STORE_PATH` (`zendure2mqtt-fabric.db` next to the config file, or in the working directory when the daemon runs from the environment alone), `MATTER_PASSCODE` (20202021), `MATTER_DISCRIMINATOR` (3840, a pointer because 0 is valid), `MATTER_VENDOR_ID` (0xFFF1), `MATTER_PRODUCT_ID` (0x8001), `MATTER_LABEL` (`zendure2mqtt`). Validation runs only when the block is enabled. It covers host:port, an existing parent directory that is not itself a directory, the passcode rule of `secure/setup.IsValidSetupPIN`, 0..4095, 1..65535, and a label of 1..32 bytes. The facade does **not** generate a passcode or discriminator when they are zero: `node.Config.Validate` refuses a zero passcode. The defaults are therefore the reference bridge's public values (`examples/reference-bridge/main.go:102-105`), and the README tells the operator to change the passcode. The PBKDF iterations (1000) and the salt are constants of the daemon.

**Device type decision.** One unit is one `device.ElectricalSensor` (0x0510, matter.js `packages/node/src/endpoints/electrical-sensor.ts:31-32`, 64-65 at `862ad75f`: PowerTopology is mandatory and needs a feature selection). The declarations:
- PowerTopology `FeatureNodeTopology`. An empty declaration is refused ("needs at least 1 of NodeTopology, TreeTopology, SetTopology"), so the kit applies no default and NODE is the smallest choice that conforms (`power-topology.element.ts:22-24`).
- ElectricalPowerMeasurement with `FeatureAlternatingCurrent`, `PowerMode` AC, one `Accuracy` entry (ActivePower, ±2 400 000 mW, `FixedMax` 1000 mW), and `ActivePower` null until the first reading. matter.js's `ElectricalPowerMeasurementServer` has no defaults.
- The battery as the device's PowerSource. `ActivePower` = `gridInputPower − outputHomePower` in mW, positive imported (`electrical-power-measurement.resource.ts:159-162`). `BatPercentRemaining` comes from `electricLevel`, in half-percent.

BatteryStorage (0x0018) was not chosen: it is a composed device type (Identify alone as its own server, with parts), which goes beyond a sensor projection.

**Tests** (daemon): `internal/fabric` uses a `fabrictest` site with `Topology.Prepare`:
- `TestUnitIsABridgedElectricalSensor` (DeviceTypeList ElectricalSensor+BridgedNode, NodeLabel, Reachable, ServerList)
- `TestLateUnitJoinsTheRunningNode`
- `TestReadingsReachTheController` (BatPercentRemaining 160, ActivePower −600000, a second reading through a subscription)
- `TestReachabilityFlips` (subscription sees Reachable false; upstream down and up again)
- `TestActivePower`
- `TestBridgePairingCodes`
- `TestNodeConfigFromTheMatterBlock` (`node.Config.Validate` passes)
- `TestSoftwareVersion`
- `TestFabricImportsOnlyTheFacade`

Elsewhere in the daemon:
- `internal/coordinator`: `TestProjectionSeesEveryReadingAndObservation`.
- `internal/config`: four tests (off by default, the store next to the config file, the env override keeping a zero discriminator, every refusal, nothing judged while disabled).
- `internal/web`: `TestHealthCarriesTheMatterPairingCode`.

**Pairing code route:** a log line at start, the retained `<name>/info` (`matter_manual_code`, `matter_qr_payload`), and the web UI status line via `/api/health`. There is no Home Assistant diagnostic entity (see Left out).

## Guide

`docs/guides/embed-in-a-daemon.md`, in the nav after "A host's view". It has eight steps, each linking its daemon commit as an absolute URL, plus "What you did not have to do" and "What a controller sees" (described from the daemon's `fabrictest` reads). It links `host-view.md` for the batch pattern rather than repeating it. Step 3 shows the typed PowerSource form and names the v0.10.8 workaround the daemon uses.

## Operations page

`docs/guides/operations.md`, in the nav after Limits. It covers:
- **Files:** `Store.Path`, the pragmas `foreign_keys` and `busy_timeout`, no journal mode set, so SQLite's rollback journal applies; what losing the file costs; backup rules linked to the persistence guide.
- **Ports:** UDP 5540, TCP only with `TCP.Incoming` on the same port, IPv6, mDNS 5353.
- **mDNS:** `_matterc`/`_matter`, the subtype responder beside the main one; avahi coexists, which the chip-tool harness relies on.
- **Logs:** `commissioning.own_window.open`, `commissioning.window.open`/`.closed`, `matter.opcreds.cmd_ok`, `commissioning.no_fabric_loaded`, `pase.*`, `matter.subscription.reestablish*`; Observers and Metrics.
- **Health:** `node.Config.Health`, `Node.Health`.
- **Limits:** fabrics 5, PASE 20 and the lockout, subscriptions 16 per fabric.
- **Failed pairing:** `DiagnosticEvents` together with `DiagnoseMDNS`.
- **Factory reset:** `RemoveFabric` against deleting the file. The UniqueID is derived deterministically, so it comes back the same.
- **Upgrading:** `SchemaSnapshotSHA256`, the store schema, CHANGELOG `Break` lines and the migration guide.

Every claim links its file as an absolute GitHub URL. Nothing is said about units or containers beyond "the daemon's packaging decides".

## Guards

| Repo | Guard | HEAD | Result |
| --- | --- | --- | --- |
| daemon | `make check` (vet, fmt-check, lint, tidy-check, race tests) | `1d552bf` | 0 (first run 2: gosec G115 in `projection.go`, folded into `0bac952` before the push) |
| daemon | `make cover-check`, `make fuzz-smoke`, `make build`, `make vuln`, `make secrets` | `d5330de` (pre-fold) | 0 each |
| daemon | every commit: `go build`, `go vet`, `go test ./...` | `3cfa56c`…`1d552bf` | ok each |
| daemon | `make licenses` | `180fab4` | local go-licenses v1.6.0, rebuilt with the local Go: exit 0 (the snap-Go build of the tool fails on the standard library locally, on main too) |
| daemon | CI on #64 | `1d552bf` | build, lint, test (linux, macOS, Windows), coverage, fuzz-smoke, CodeQL pass; **security fails on GO-2026-6809** (crypto/x509, fixed in Go 1.27.3, reached from `internal/zendure/cloud`), a standard-library finding independent of this PR; its licence step did not run after it |
| go-fabric | `make ci` (local go1.27.3) | `1746f7b5` | 0 (a first run hit a transient "no space left on device" in the build cache and one `TestConnectionErrorClasses` failure in controller/commissioner under that load; the package passes alone, `-count=3`, and in the rerun) |
| go-fabric | `go test ./script/...` (docscheck reads the new pages) | `1746f7b5` | 0 |
| go-fabric | `mkdocs build --strict` | `1746f7b5` | 0 |
| go-fabric | `make race` (once, for the two code commits) | `1746f7b5` | 0 |

The local toolchain is go1.27.3 (go.mod says 1.27.2), which is why the daemon's local `make vuln` passed while its CI on 1.27.2 reports GO-2026-6809; the coordinator's go-zendure2mqtt#65 pins 1.27.3. The report commit changes notes only; no guard was rerun for it.

## Differences

1. **Facade gap: PowerSource on a bridged device by typed declaration** (`092ccc47`). At v0.10.8 `device.NewElectricalSensor(…, powersourcedef.Declare{…})` was refused: "ElectricalSensor has no server cluster requirement for cluster 0x002F". The `device/measurement.go` route takes a `contract.MeasurementSource`, not a typed declaration. The change follows the brief's rule:
   - `device/base.go` `withBridgedPowerSource` reads the BridgedNode requirements under the conformance `BridgedPowerSourceInfo` alone (`bridged-node.element.ts:23` at `862ad75f`; the condition is `base.element.ts:40`). It accepts the declared cluster and states the condition in the device's `Conditions`, which reach `endpoint.Spec.DeviceConditions` and so `StatedConditions`.
   - It adds `Device.PowerSource()`, and `AddPart` refuses a part with such a declaration.
   - Tests: `TestBridgedDeviceDeclaresPowerSource` (OnOffLight through `fabrictest`: BatPercentRemaining 160, EndpointList `[ep]`, defaults, ServerList, `Set` reaching the read), `TestElectricalSensorDeclaresPowerSource`, `TestPartRefusesBridgedPowerSource`, and `TestBridgedPowerSourceServers` (the parity case against the snapshot).
   - CHANGELOG `### Added`.
2. **Facade defect: a measurement battery's changes never reached a subscriber** (`9bd655d5`). The battery rides as `endpoint.Spec.PowerSource` (`device.Measurement` with a `MeasurementBattery` source beside another class), so it is neither the endpoint's Source nor its Measurement. `cluster/measurement.PowerSourceServer` did not forward its source's `ChangeNotifier` (`ElectricalPowerServer` does, `cluster/measurement/measurement.go:886`), so the bridge wired nothing. A subscriber saw the level only on a read. The daemon's subscription test found it.
   - The fix: `PowerSourceServer.OnMatterValueChanged` forwards (matter.js `Datasource.ts:268-270` cited).
   - Test: `TestMeasurementBatteryReportsItsChanges`, confirmed failing without the fix.
   - CHANGELOG `### Fixed`.
3. **The daemon stays on v0.10.8** with both workarounds: `device.Measurement(battery)` plus `device.WithChangeNotifier(battery)`. `BatChargeLevel` stays `Ok` and there is no `BatChargeState`. The typed `powersourcedef.Declare` waits for a release carrying 1 and 2.

## Decisions

- Units are added at `Start` from `Source.Devices()`. A unit first seen in a reading or a `DeviceReachable` is added then. `MarkComplete` runs once, on the first `UpstreamUsable(true)`, because the cloud's list exists only after login. Units are never removed: the daemon's `state` and coordinator never drop a serial.
- The reachability of a unit is `DeviceReachable && upstream usable`. On upstream up, each unit gets its own last word back, and an unknown unit counts as reachable.
- The projection hooks into `Coordinator.publish`, so the re-read after an MQTT write projects too. The coordinator stays the backend's only observer and forwards to `Deps.Fabric`.
- The node starts before `coord.Run` and stops in a defer that runs before the MQTT lifecycle's stop. It also stops on an early return.
- `SoftwareVersion` is encoded as X·10⁶+Y·10³+Z, and is 0 for a development build.
- The stable key is `zendure:<SN>`. The NodeLabel comes from `hass.DeviceName`, as Home Assistant names the unit.
- `make licenses` ignores `modernc.org/mathutil` by name. It is BSD-3-Clause, but go-licenses cannot classify it and logs an error.

## Left out

- Writes from Matter. The endpoint is a sensor, and the README and guide say so.
- Home Assistant add-on options for `MATTER_*`. The add-on maps its own options; `ZENDURE_MATTER_*` environment variables work, and the add-on would need host networking.
- A Home Assistant diagnostic entity for the pairing code. The code is in the log, `<name>/info` and the web UI instead.
- The typed PowerSource in the daemon. It waits for the release (Differences 3).
- A real pairing with Apple Home or Home Assistant. That is the owner's manual step, listed in both PR bodies and not claimed.
- The daemon's CI security job fails on GO-2026-6809 (Go 1.27.3), which is not this PR's subject.

## Questions

1. Release go-fabric with Differences 1 and 2 (v0.10.9), then a daemon follow-up moves to `powersourcedef.Declare` with `BatChargeLevel`/`BatChargeState` and drops `battery.go`?
2. The kit's default PowerSource leaves the mandatory `Order` and (with BAT) `BatReplacementNeeded` unset. A wildcard read then answers UnsupportedAttribute for them until the host states them. matter.js `PowerSourceServer.initialize` does not set them either (`PowerSourceServer.ts:37-62`). Is that a findings-register entry, or the host's statement by design?
3. Reading a FeatureMap through `fabrictest.ReadCluster` needs `cluster/spec.DecodeAttribute`, below the daemon's facade rule. Should `<cluster>def.StateFrom` or `fabrictest` expose the global attributes?

## by_design candidates

None. The two gaps were defects and are fixed (Differences 1, 2).
