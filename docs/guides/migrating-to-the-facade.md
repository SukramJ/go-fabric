# Migrating to the facade

For a host that imports packages below `node/` today. The worked example is
[OpenCCU-Loom](https://github.com/SukramJ/openccu-loom) at its go-fabric
`v0.6.0` pin (`go.mod:36`); its locations are given as `file:line` in that
repository. Each row names the facade route, or the section of
[Advanced seams](advanced-seams.md) when the symbol stays below the facade.

## Symbol → route

| Package | Symbol (loom) | Route |
| --- | --- | --- |
| `bridge` | `StartHealthProbe`, `DefaultProbeInterval` (`cmd/openccu-loom/daemon_matter.go:571`) | `Config.Health{Interval, OnSample}`; `node.DefaultProbeInterval` |
| `bridge` | `HealthSample` (`daemon_matter.go:1433`, `:1437`) | `node.HealthSample`; `Node.Health` returns the last one. `OnSample` gets every verdict; an unhealthy one is what `RecordUnhealthy` received |
| `bridge` | `RandomPasscode`, `RandomSalt` (`daemon_matter.go:282`, `:297`; `matter_window_adapter.go:153`, `:157`), `RandomDiscriminator` (`matter_window_adapter.go:149`) | `node.NewCommissioningCredentials`; `Commissioning.WindowCredentials(timeout)` for the ephemeral window (`matter_window_adapter.go:165-171`) |
| `bridge` | `ErrAdministratorWindowOpen`, `ErrCommissioningWindowAlreadyOpen` (`matter_window_adapter.go:208`), `ErrCommissioningWindowNotConfigured` (`matter_status_adapter.go:105`) | `node.ErrAdministratorWindowOpen`, `node.ErrCommissioningWindowAlreadyOpen`, `node.ErrCommissioningWindowNotConfigured` (the same values); `node.ErrCommissioningBlocked` |
| `bridge` | `Snapshotter` (`daemon_matter.go:915`), `TCPConfig` (`daemon_matter.go:56`) | `node.Snapshotter`, `node.TCPConfig` |
| `bridge` | `*CommissioningWindow` (`matter_status_adapter.go:33`, `matter_window_adapter.go:71`) | `node.CommissioningWindow`, the type `Node.CommissioningWindow` returns |
| `bridge` | `LocalAddr` (`daemon_matter.go:562`, `matter_status_adapter.go:47`) | `Node.LocalAddr` |
| `bridge` | `*Bridge` with `Topology` (`daemon_matter.go:1315`, `matter_window_adapter.go:98`), `NotifyDeviceReachable` (`daemon_matter.go:974`), `ControllerRTT` (`daemon_matter.go:584`) | stays: [`bridge`](advanced-seams.md#bridge) through `Node.Bridge` |
| `store` | `New`, `*Store` (`daemon_matter.go:373`, `:137`) | `Config.Store.Path` for a file the node opens; a shared handle stays [`store`](advanced-seams.md#store) (`Store.Credentials`) |
| `store` | `FabricRecord` (`internal/north/rest/handlers/matter.go:231`) | `Node.FabricInfos`: `FabricInfo` carries index, fabric and node id, root public key, compressed fabric id, `VendorID` and `Label` |
| `store` | `SettingUniqueID` (`daemon_matter.go:520`, a comment) | `Node.UniqueID` |
| `secure/setup` | `IsValidSetupPIN` (`daemon_matter.go:277`, `internal/config/validate.go:167`), `MaxDiscriminator` (`validate.go:158`) | `node.ValidateSetupPIN`, `node.MaxDiscriminator`; `Config.Validate` checks both |
| `secure/setup` | `Payload`, `QRCode`, `ManualCode`, `DiscoveryOnIP` (`matter_window_adapter.go:176-187`) | `Node.PairingCodesFor(WindowCredentials)` for a window opened with the host's credentials; `Node.PairingCodes` for the configured ones |
| `secure/setup` | the same (`internal/north/rest/handlers/matter.go:60-72`, without a node) | stays: [`secure/…`](advanced-seams.md#secure) |
| `secure/spake2` | `PBKDFMinSaltSize`, `PBKDFMaxSaltSize` (`daemon_matter.go:289`), `IterationsMin`, `IterationsMax` (`internal/config/validate.go:163`, `internal/config/config.go:1087`) | `node.PBKDFSaltMin`, `node.PBKDFSaltMax`, `node.PBKDFIterationsMin`, `node.PBKDFIterationsMax` |
| `secure/operational` | `Manager` (`daemon_matter.go:153`, `:1193`) | `node.SessionManager` (the same type) |
| `im/subscription` | `Manager` (`daemon_matter.go:154`, `:1194`) | `node.SubscriptionManager` (the same type); [`im/subscription`](advanced-seams.md#imsubscription) |
| `secure/attestation` | `LoadChainFiles`, `ChainFiles`, `ErrNoChainConfigured` (`daemon_matter.go:318`, `:329`) | `Config.Attestation.Files` (`node.AttestationFiles`); loom starts on a chain that does not load and blocks commissioning (`daemon_matter.go:480-491`, `:552-554`): `Attestation.BlockCommissioningOnError`, the error in `Node.AttestationError`. Files naming nothing keep the test chain, so `ErrNoChainConfigured` has no counterpart |
| `mdns` | `NewZeroconf`, `NewSubtypeResponder`, `NewNoop`, `Advertiser` (`daemon_matter.go:81-121`) | a nil `Config.Advertiser`; `Network.MDNS{Disabled}` for loom's `noop` |
| `mdns` | `IsEnabled` (`daemon_matter.go:1263`), `Diagnose` (`:1289`), `Advertiser.Active` (`:1265`) | `Node.MDNSEnabled`, `Node.DiagnoseMDNS`, `Node.MDNSServices` (`node.MDNSFinding`, `node.MDNSService`) |
| `endpoint` | `SourceKey` (`internal/store/matterendpoint/sourcekey.go:68`), `Topology` (`daemon_matter.go:916`) | `device.SourceKey`, `node.Topology`; `device.AvailabilityProbe` for `Options.Reachable` (`assembler.go:861`) |
| `endpoint` | `Assembler`, `New`, `Spec`, `ComposeNodeLabel` (`assembler.go:155`, `:184`, `:627`, `:641`); `endpoint/sqlitestore` (`internal/store/matterendpoint/store.go:10`) | stays: [`endpoint`](advanced-seams.md#endpoint) until the device kit replaces the host's assembler |
| `groups` | `NewManager` (`daemon_matter.go:388`, `Config.Groups` at `:534`) | a nil `Config.Groups` builds the manager; `Node.Groups` returns it. With an own assembler it stays [`groups`](advanced-seams.md#groups) |
| `bootid` | `EnableRotation` (`daemon_matter.go:409`) | `Config.BootID.RotateUniqueIDs` |
| `diagevent` | `Event` (`internal/north/rest/handlers/matter.go:556`) | `node.DiagnosticEvent`; [`diagevent`](advanced-seams.md#diagevent) |
| `cluster/core` | `SoftwareVersionFromString` (`daemon_matter.go:527`) | `node.SoftwareVersionFromString` |
| `cluster/core` | `RegulatoryIndoorOutdoor` (`daemon_matter.go:506`) | `node.RegulatoryIndoorOutdoor` for `Commissioning.LocationCapability` |

## What a migrating host meets

1. **`Node.Fabrics` is deprecated.** It returns `[]store.FabricRecord`;
   `Node.FabricInfos` returns `[]node.FabricInfo`. `Fabrics` keeps working
   and is removable in `v0.11.0`. The compressed fabric id is
   `CompressedFabricID` on `FabricInfo`.
2. **A nil `Config.Advertiser` advertises.** Up to `v0.9.0` it was
   `mdns.NewNoop()`; now the node builds Zeroconf with the subtype
   responder. A host that relied on the old default sets
   `Network.MDNS.Disabled`.
3. **Accessor return types** of the typed cluster layer change, landing in
   `v0.9.2`.

## Verify

The import list of the host's binary, below the facade:

```sh
go list -deps ./cmd/openccu-loom | grep '^github.com/SukramJ/go-fabric/'
```

`-deps` lists what the facade imports too; the host's own imports are the
packages its source names (`go list -f '{{join .Imports "\n"}}' ./...`).

The device mapping runs on `fabrictest`: a `fabrictest.NewSite`, a
`Site.CommissionedPair` whose `Topology.Bridged` holds the host's
endpoints (or `Topology.Snapshotter` for its own assembler, and
`Topology.Configure` for the rest of its `node.Config`), and the pair's
controller reading and writing them over CASE —
[Testing in process](testing-in-process.md).

*Source: `node/facade.go`, `node/health.go`, `node/credentials.go`, `node/attestation.go`, `node/mdns.go`, `node/observers.go`, `node/node.go`, `fabrictest/device.go:45-75`, `fabrictest/site.go:84`, `fabrictest/controller.go:450`.*
