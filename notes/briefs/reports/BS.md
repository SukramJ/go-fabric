# Report BS — milestone 4, phase 20a: the node facade complete

Branch `m4/phase-20a` from main 7084a60 (v0.9.0 + concept v6). Implementer:
Opus, one agent. Every loom symbol of the brief's table has a facade route
or a section on the advanced-seams page; the reference daemon's `main.go`
imports `cluster/core`, `cluster/spec/fixedlabel`, `contract`,
`controller/dcl`, `node` and `schema` only. Two pages added
(`docs/guides/advanced-seams.md`, `docs/guides/migrating-to-the-facade.md`,
the second by the owner's addition to the brief). After the owner's API standard of the wave,
one refactor commit (374dd3a) made each route one shape — see Fixes.

## Routes

| Loom symbol | Built (file:line) |
| --- | --- |
| `bridge.StartHealthProbe`, `HealthSample`, `DefaultProbeInterval` | `Config.Health{Interval, OnSample}` (`node/config.go:163`, `node/health.go:27`), `node.HealthSample`, `node.DefaultProbeInterval` (`node/health.go`), `Node.Health` (`node/health.go:95`). The probe is `bridge.StartHealthProbeWithClock` on `Config.Clock`, started in `Start` after the bridge, stopped in `Stop` before it. It runs when `OnSample` is set or `Interval` > 0 |
| `bridge.RandomPasscode`, `RandomSalt`, `RandomDiscriminator` | `node.NewCommissioningCredentials` (`node/credentials.go:26`, `WindowCredentials` `:51`): the three bridge functions, `PBKDFIterationsMin` iterations; `Commissioning.WindowCredentials(timeout)` turns it into one `OpenBasicWindow`'s credentials (loom's ephemeral window converted `int` → `uint32` itself with a `gosec` nolint, `matter_window_adapter.go:165-171`) |
| window errors | `node.ErrAdministratorWindowOpen`, `ErrCommissioningBlocked`, `ErrCommissioningWindowAlreadyOpen`, `ErrCommissioningWindowNotConfigured` (`node/facade.go:64-75`), the bridge's values |
| `bridge.Snapshotter`, `TCPConfig`, `Topology`, `LocalAddr` | aliases `node.Snapshotter` (`node/facade.go:29`), `Topology` (= `endpoint.Topology`), `TCPConfig`, plus `CommissioningWindow` (loom names `*bridge.CommissioningWindow`); `Config.Topology`, `Network.TCP`, `Node.CommissioningWindow` declared through them. `Node.LocalAddr` (`node/node.go:784`) |
| `store.New`, `Store`, `SettingUniqueID` | `Config.Store.Path` (`node/config.go:369`, `node/store_path.go`): the node opens the file (modernc.org/sqlite, the daemon's pragmas), treats it as `DB`, closes it in `Stop` and on a failed `New`; `Path` excludes `DB`. `Node.UniqueID` (`node/node.go:872`) |
| `store.FabricRecord` | `Node.FabricInfos` (`node/node.go:856`); `FabricInfo` gains `VendorID`, `Label` (`node/observers.go`); `Fabrics` `Deprecated:`, removable in v0.11.0 (`node/node.go:849`); the 59 in-repo callers moved |
| `secure/setup`, `secure/spake2` constants | `node.ValidateSetupPIN` (`node/facade.go:115`), `MaxDiscriminator` (`:81`), `PBKDFIterationsMin/Max`, `PBKDFSaltMin/Max`; `Config.Validate`'s doc names them |
| `secure/setup.Payload`, `QRCode`, `ManualCode`, `DiscoveryOnIP` | `Node.PairingCodesFor(WindowCredentials)` (`node/credentials.go:69`): loom's window adapter encodes the credentials of a window it opened itself. `PairingCodes` shares the encoder |
| `secure/operational.Manager`, `im/subscription.Manager` | aliases `node.SessionManager`, `node.SubscriptionManager`; `Sessions()`/`Subscriptions()` return them (same types). Not interfaces: see Differences |
| `secure/attestation.LoadChainFiles`, `ChainFiles`, `ErrNoChainConfigured` | `Attestation.Files *AttestationFiles` (an alias of `attestation.ChainFiles`; `node/config.go:381`, `:388`, `node/attestation.go:17`, `AttestationError` `:48`): `New` loads it, files naming nothing keep the test chain, a failed load fails `New`; `Attestation.BlockCommissioningOnError` keeps the node on the test chain with commissioning refused (`Node.SetCommissioningBlocked` at the end of `New`) and `Node.AttestationError` reports the error — loom's semantics, one route. `Files` excludes `Chain`/CD, the flag needs `Files` (`Validate`). `ErrNoChainConfigured` is not re-exported: no facade call returns it |
| `mdns.NewZeroconf`, `NewSubtypeResponder`, `NewNoop`, `IsEnabled`, `Diagnose` | a nil `Config.Advertiser` is Zeroconf with the subtype responder (`node/mdns.go`, `node/node.go:157`), `Network.MDNS{Disabled, HostName}` (`node/config.go:239`, `node/mdns.go:14`); `Node.MDNSEnabled` (`:75`), `MDNSServices` (`:79`), `DiagnoseMDNS` (`:85`); `MDNSFinding`, `MDNSService` aliases. `### Changed` |
| `endpoint.SourceKey`, `AvailabilityProbe` | `device.SourceKey`, `device.AvailabilityProbe` (`device/options.go:54`, `:58`); `Options` declared through them |
| `groups.NewManager` | nothing to build (`New` builds it when nil); `Config.Groups` doc names the seam |
| `bootid.EnableRotation` | `Config.BootID.RotateUniqueIDs` (`node/config.go:160`, `:191`); `New` calls it right after `Validate` (`node/node.go:138`), before the store opens |
| `diagevent.Event` | `node.DiagnosticEvent` |
| `cluster/core.SoftwareVersionFromString`, `RegulatoryIndoorOutdoor` | `node.SoftwareVersionFromString` (`node/facade.go:107`), `node.RegulatoryIndoor/Outdoor/IndoorOutdoor`; `Commissioning.LocationCapability` already existed, no new field |
| `schema`, `eligibility`, `cluster/<server>` | no change; named as the typed surface on the seams page |
| (owner's addition) | `docs/guides/migrating-to-the-facade.md`: symbol → route table by package with loom `file:line`, the three notes, a verify section; in the nav and the docs CHANGELOG entry |

Commits: a87f465 (names), c6439fd (credentials), 64b0314 (health,
LocalAddr), 42f37aa (Store.Path), 3a3ba42 (FabricInfos, UniqueID,
deprecation), 87fa656 (attestation), 1f293ce (mDNS), 76d5117 (BootID),
3862e0b (daemon), 676712e (docs), 374dd3a (API review, below). Each route
commit carries its test and the regenerated reachability snapshot.

Tests: `node/facade_routes_test.go` (errors are the bridge's, bounds,
`ValidateSetupPIN`, aliases, credentials, `PairingCodesFor`, `FabricInfos`,
`UniqueID`), `node/health_test.go` (fake clock: a sample at Start, one per
interval, none after Stop), `node/store_path_test.go` (opens, closes,
second node reads the first's setting; Validate), `node/attestation_internal_test.go`,
`node/mdns_internal_test.go` (type asserted, never started),
`node/bootid_internal_test.go` (a spy; the real rotation is process-wide).

## Still imported by the daemon

`main.go`: none of `bridge`, `mdns`, `secure/*`, `store`, `bootid`,
`endpoint`. The package's other files:

| File | Package, symbol | Reason |
| --- | --- | --- |
| `fleet_node.go` | `bridge.Bridge`, `endpoint.StringKey` | `attachNodeProviders` and `endpointByKey` (moved here from `main.go`) read the live topology through `Node.Bridge()`: advanced seam |
| `fleet.go`, `media.go`, `control_actions.go` | `endpoint` | the fleet's `StableKey`s (`endpoint.StringKey`) and endpoint types; the device kit (BT/BU) owns that layer |
| `fleet_climate.go` | `store.Store` | the thermostat's preset store over `Node.Store()`: advanced seam (`store`) |

`persist.go` lost `openDB`, the driver import and `store` (a two-method
`settings` interface over `Node.Store()`); `attestation.go` (now
`attestationConfig`: the four paths in `Attestation.Files`) and `tcp.go`
import `node` only.

## Loom reads

At `openccu-loom` da714cf3, pinning go-fabric v0.6.0 (`go.mod:36`):

- `LocalAddr`: `cmd/openccu-loom/daemon_matter.go:562` logs `bridge.LocalAddr()`, `matter_status_adapter.go:47` reports it — the listener's address → `Node.LocalAddr`.
- `SettingUniqueID`: production code names it in a comment only (`daemon_matter.go:520`); a test pins a value through it (`daemon_matter_facade_test.go:305`) → `Node.UniqueID` to read back, nothing to set.
- Window payload: `matter_window_adapter.go:148-199` draws its own credentials (`:149-157`), opens `OpenBasicWindow` (`:172`) and encodes them with `secure/setup` (`:176-187`) → `PairingCodesFor`. The configured-credentials path already uses `PairingCodes` (`:130`). `handlers/matter.go:60-72` encodes without a node: stays `secure/setup`.
- Manager types: loom names both as struct field types (`daemon_matter.go:153-154`, `:1193-1194`) and calls `Sessions`, `Occupancy` (`:1206`, `:1232`), `CountBySession` (`:1204`) → aliases.
- Fabric fields: `handlers/matter.go:283-292` reads FabricIndex, FabricID, NodeID, VendorID, Label, CompressedID, RootPublicKey → `FabricInfo` gains VendorID, Label.
- Attestation: `daemon_matter.go:318-336` loads, and on an error starts anyway and blocks commissioning (`:480-491`, `:552-554`) → `Attestation.BlockCommissioningOnError`, `Node.AttestationError`.
- Health: `daemon_matter.go:571` starts the probe after `Start`, `:1433-1439` adapts `Record`/`RecordUnhealthy`.

## Fixes

Nothing found broken on the way. One refactor commit after the owner's
API standard of this wave (374dd3a):

- **One attestation route.** The wave first had `Attestation.Files` beside an exported `node.LoadAttestation` (and the daemon used the latter); now `Files` is the only form, its failure mode a field (`BlockCommissioningOnError`) with `Node.AttestationError`. `LoadAttestation` and the re-exported `ErrNoChainConfigured` are gone (unreleased). `AttestationFiles` is an alias of `attestation.ChainFiles` instead of a copied struct.
- **`SoftwareVersionFromString` is a function**, not a re-exported function variable a caller could reassign.
- **`Commissioning.WindowCredentials(timeout)`**: the two credential structs (`Commissioning`, `WindowCredentials`, which differ in the iteration type) meet in one conversion instead of each host's own.
- **One set of commissioning checks** in `node/config.go` (`validSetupPIN`, `errSetupPIN`, `checkDiscriminator`, `checkPBKDF` against the `node` constants): `Config.Validate`, `WindowCredentials.validate` and `ValidateSetupPIN` share them; the copied checks against `secure/setup` and `secure/spake2` in `config.go` and `commissioning.go` are gone.
- `New`: the failure cleanup (`abandon`) and the advertiser default (`resolveAdvertiser`) are methods, which kept `New` under the cyclomatic limit.

## Guards

Final run at HEAD 374dd3a (summary log
`scratchpad/bs/guards/summary.log`), before and after:

```
HEAD before 374dd3a 17:23:37
exit 2 cite-check 27s
exit 0 testparity-check 1s
exit 0 reachability-check 90s
exit 0 ci 1748s
exit 2 race 1903s
exit 0 chiptool-test 129s
HEAD after dc4401d 18:28:35
```

- `ci` includes `cover-check`, `lint` and `bluez-check`: all passed.
- `cite-check` failed only on stale artefacts: the import change in `node/config.go` shifted three pre-existing drift entries by a line. No hard error, drift 264 as before; the regenerated artefacts are dc4401d. Earlier, the routes' own citations (the `// Mirrors` of the mDNS default, `node/mdns.go`) moved the counts with drift unchanged; they were committed in 374dd3a.
- `race` failed on three package timeouts at 10 min — `node` (in `TestOtaParityMatterJS`), `fabrictest` (`TestEventsAndSubscribe`, `TestNetworkLossAndDelay`, `TestControllerRestart`) and `controller/client` (`TestControllerDiscover`) — with no `DATA RACE`, under a load average of about 25 on 5 cores (two other agents' race and cover suites ran beside it). The three packages rerun alone under `-race`:

```
HEAD before dc4401d9 18:28:58 race rerun, load 13.16
exit 0 race ./node/ 689s load 10.89
exit 0 race ./fabrictest/ 65s load 8.85
exit 0 race ./controller/client/ 508s load 3.58
HEAD after dc4401d9 18:50:01
```

- The rerun ran with `-timeout=30m`; no `DATA RACE` in any of the three logs. `node` needed 689 s alone at a load of 11, above `make race`'s 10-minute package default; `ci`'s non-race run of `node` took 268 s under the same contention. The wave's `node` tests add a few seconds under `-race` (the advertiser test about 3 s, the rest under 2 s), so the margin is the package's, not this wave's — see Questions.
- `reachability-check`: the snapshot moved with every route commit by the new exported names and their test entry points only; unreached stays 5, no ratchet moved.
- `testparity-check`: unchanged, as the brief expects.
- `chiptool-test` (the mDNS wiring moved): exit 0 — `TestChipToolSuite`, `TestGoControllerCommissionsReferenceBridge` and `TestGoControllerSubscribesReferenceBridge` pass, `TestMatterJSController` skips as before. The daemon advertised through the node's own Zeroconf and subtype responder (`--mdns-os-hostname` → `Network.MDNS.HostName`). No families: composition and FeatureMap did not move.
- `mkdocs build --strict`: exit 0 (run locally after the docs commit).

## Differences to the brief

- **Sessions/Subscriptions are aliases, not interfaces.** v0.9.1 is a patch release, and README §"The deprecation window" forbids a signature change in one; returning an interface from `Node.Sessions()` would also break loom, which stores the result in a `*operational.Manager` field. `node.SessionManager` and `node.SubscriptionManager` give loom the names without the import and change nothing.
- **`store.New(path)`** does not exist (`store.New` takes a `*sql.DB`); `Store.Path` opens the file with modernc.org/sqlite and the daemon's pragmas and then follows the `DB` path (both schemas, both stores). `node` now imports the driver.
- **Health "no reader"**: a probe runs when `OnSample` is set or `Interval` > 0; a host that only reads `Node.Health` sets `Interval` (`DefaultProbeInterval`).
- **Attestation**: `AttestationFiles` is an alias, not a node struct; `ErrNoChainConfigured` is not re-exported (no facade call returns it); `BlockCommissioningOnError` and `Node.AttestationError` added for loom's start-and-block semantics. The daemon's start-up error no longer names `--attestation-dir` (it names the file: `node: attestation files: …`).
- **Added beyond the table**: `node.CommissioningWindow`, `node.Topology` aliases, `Node.MDNSServices` with `MDNSService` (loom's mDNS reporter lists the advertised records, `daemon_matter.go:1265`), `MDNS.HostName` (the daemon's `--mdns-os-hostname`), `Node.UniqueID`, the regulatory constants.
- **Deprecating `Fabrics` moved 59 call sites** in 38 files (staticcheck SA1019 is on), among them test files under `bridge/bridgetest`, `cluster/core`, `controller/…`, `fabrictest` and `examples/reference-controller` — call-site renames, no behaviour; `fabrictest/device.go` and one helper each in `fabrictest/client_test.go` and two node tests now use `FabricInfo` (`CompressedFabricID`).
- **`device/subscriber_test.go`** sets `Network.MDNS.Disabled` (one line): it starts a node with a nil advertiser, which would now publish on the LAN.
- **No `RegulatoryLocation` field**: `Commissioning.LocationCapability` exists and loom sets it.

## Decisions the owner should check

- `node` links modernc.org/sqlite (registers the driver name `sqlite`): a host that registers another driver under that name panics at init. `Store.DB` stays for such a host.
- The salt follows matter.js (owner's review of #197): `bridge.RandomSalt` draws 32 bytes (`DeviceCommissioner.ts:134`, `CommissioningClient.ts:524` at 862ad75f), and `NewCommissioningCredentials` keeps using it — one function. `### Changed` in the CHANGELOG; the two bridge tests that asserted 16 now assert 32.
- The node's own advertiser is built in `New`, so the subtype responder's sockets open at `New`, as the daemon opened them before `New`.
- `DiagnoseMDNS` returns nil for a Noop advertiser (loom's reporter skips the diagnosis then) rather than the `no_services` finding.
- `Fabrics` removable in v0.11.0 (marked in v0.9.1; the concept said v0.10.0, which the README's "second minor after" rule does not allow).

## Left out

- Loom's own migration (A.6). The quickstart example keeps its explicit advertiser (not this brief's file).
- `Node.Topology()` / reachability / controller RTT stay on `Node.Bridge()` (seam).
- Lower types still in `node`'s signatures without an alias, outside this brief's table: `Config.DeviceTypeValidation` (`endpoint.DeviceTypeValidationMode`), `Node.Peers` (`*peer.Set`), `Node.Bindings` (`*binding.Manager`). The seams by design — `Config.Advertiser`, `Config.Groups`, `Store.Credentials`/`Endpoints`, `Attestation.Chain`, `Node.Bridge`, `Node.Store`, `Node.Groups`, `NewEndpointPersistence(*store.Store)` — are on the seams page.

## Questions

Answered in the review of #197:

- `node` under `-race` near the 10-minute package timeout on a loaded machine (689 s alone at load 11): the timeout stays; test runtime under load is a gate-1 audit item.
- `Node.Topology` (loom reads `Bridge().Topology()` at `daemon_matter.go:1315`, `matter_status_adapter.go:49`, `matter_window_adapter.go:98`): goes to brief BU.
- `Config.DeviceTypeValidation`, `Node.Peers`, `Node.Bindings` exposing lower types: gate-1 findings, not this wave.
- The salt: follows matter.js, 32 bytes (Decisions).

## by_design candidates

None.
