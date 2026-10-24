# Brief BS — milestone 4, phase 20a: the node facade complete

Date 2026-10-20. Roadmap milestone 4 (`docs/roadmap.md`, phase 20) after
[concept v6](../../docs/history/concept-v6-embedding-layer.md) Teil A.2 (owner
decisions 0.1–0.4, 2026-10-20) and the phase-20 read map
(`notes/briefs/reads/read-m4-phase-20.md` §1, §2: every loom symbol with
its file, the facade's exported surface with lines). go-fabric `v0.9.0`.
First wave of phase 20 (`v0.9.1`), parallel to brief BT (disjoint paths:
BT owns `script/clustergen`, `cluster/spec/*`, `script/devicegen`,
`device/`; this brief owns `node/`, `docs/`, `examples/reference-bridge/main.go`).
Briefs BO–BR are the template for the report and the guards.

**Model:** Opus implements. No change inside `secure/`, `bridge/`,
`mdns/`, `store/` beyond what a route needs (a re-export, an alias, a call
the facade makes); a change of behaviour in one of them is a question, not
a commit. The one security-adjacent route is attestation loading, which is
a call to the existing `secure/attestation.LoadChainFiles`
(`secure/attestation/files.go:77`), nothing more.

## Goal

Every symbol OpenCCU-Loom's product code reaches below `node/` today
(read map §1, the table in concept v6 A.2) has a facade route or is a
documented advanced seam. Measured on the reference daemon in this wave:
after the wave, `examples/reference-bridge/main.go` imports none of
`bridge`, `mdns`, `secure/*`, `store`, `bootid` for the symbols the table
names (what it still imports is listed in the report with the reason).

## Scope — the routes (concept A.2, item by item)

| Loom symbol | Build |
| --- | --- |
| `bridge.StartHealthProbe`, `HealthSample`, `DefaultProbeInterval` (`bridge/health_probe.go:23`, `:41`, `:66`, `:77`) | `node.Config.Health{Interval time.Duration; OnSample func(HealthSample)}`; `type HealthSample = bridge.HealthSample`; `Node.Health() (HealthSample, bool)` the last sample. The probe runs on `node.Config.Clock` (`StartHealthProbeWithClock`). Zero `Interval` = `DefaultProbeInterval`; nil `OnSample` and no reader = no probe. |
| `bridge.RandomPasscode`, `RandomSalt`, `RandomDiscriminator` (`bridge/commissioning_window.go:945-1000`) | `node.NewCommissioningCredentials() (Commissioning, error)` filling passcode, discriminator and salt with the existing functions (read `node.Commissioning`'s fields first; the PBKDF iteration default stays what `Config.Validate` accepts). |
| `bridge.ErrAdministratorWindowOpen`, `ErrCommissioningBlocked` (`:333`, `:342`), the window errors loom's adapter compares (`matter_window_adapter.go`: `ErrCommissioningWindowAlreadyOpen`, `ErrCommissioningWindowNotConfigured` — read `bridge/` for their definitions) | `var Err… = bridge.Err…` in `node` (one exists: `node/commissioning.go:213` uses `bridge.ErrAdministratorWindowOpen` — expose it as `node.ErrAdministratorWindowOpen`). |
| `bridge.Snapshotter`, `bridge.TCPConfig`, `bridge.Topology`, `bridge.LocalAddr` (fields and helpers loom names) | type aliases in `node` (`type Snapshotter = bridge.Snapshotter`, …); `node.Config.Topology` and `.TCP` keep their types (an alias is the same type: no break). `LocalAddr`: read loom's use (`daemon_matter.go`) and route it (`Node.LocalAddr()` if it is the listener's address). |
| `store.New`, `store.Store`, `store.SettingUniqueID` | `node.Store.Path string`: `node.New` opens `store.New(path)` when `Credentials` is nil and `Path` is set, `Stop` closes it. `SettingUniqueID`: read loom's use; if it reads or sets the node's UniqueID, `Node.UniqueID()` / `Config.BasicInformation.UniqueID` (check what exists). |
| `store.FabricRecord` from `Node.Fabrics()` (`node/node.go:774`, loom `handlers/matter.go:231`) | `Node.FabricInfos(ctx) ([]FabricInfo, error)`; `FabricInfo` (`node/observers.go:77`) gains the fields loom's handler shows (read `handlers/matter.go:200-260`: label, vendor id, …; take them from `store.FabricRecord`). `Fabrics()` stays and is `### Deprecated` with the README window (removal named for the release after the window). |
| `secure/setup.IsValidSetupPIN`, `MaxDiscriminator`; `secure/spake2.PBKDFMinSaltSize`, `PBKDFMaxSaltSize`, `IterationsMin`, `IterationsMax` | `node.ValidateSetupPIN(pin uint32) error` (wrapping `setup.IsValidSetupPIN`), `const node.MaxDiscriminator = setup.MaxDiscriminator`, `node.PBKDFIterationsMin/Max`, `node.PBKDFSaltMin/Max` — values from the two packages, never literals. `Config.Validate` (`node/config.go:388`) already checks them: say so in the doc comment. |
| `secure/setup.Payload`, `QRCode`, `ManualCode`, `DiscoveryOnIP` (loom's window adapter and REST handler) | `Node.PairingCodes()` exists (`node/node.go:789`); read the adapter: if it builds a payload for a window it opened with its own credentials, add `node.PairingCodesFor(WindowCredentials)`; otherwise document the existing accessor. |
| `secure/operational.Manager`, `im/subscription.Manager` (types of `Node.Sessions()`, `Node.Subscriptions()`) | read loom's use (`daemon_matter.go`); where loom names the type, an interface in `node` (`node.Sessions`, `node.Subscriptions`) with the methods loom calls, the accessors returning it; the managers stay reachable through `Node.Bridge()`. If loom only calls methods on the returned value, no change — say so. |
| `secure/attestation.LoadChainFiles`, `ChainFiles`, `ErrNoChainConfigured` | `node.Attestation.Files *AttestationFiles` (a `node` struct with the fields of `attestation.ChainFiles`, read `files.go`); `node.New` loads it with `LoadChainFiles` and fills `Chain` and the CD; `ErrNoChainConfigured` re-exported. `Chain` stays for a host with its own chain. |
| `mdns.NewZeroconf`, `NewSubtypeResponder`, `NewNoop`, `IsEnabled`, `Diagnose` (`mdns/zeroconf.go:133`, `subtype_responder.go:101`, `advertiser.go:49`, `reannounce.go:45`, `diagnose.go:44`) | `node.Config.Network.MDNS{Disabled bool}`; a nil `Config.Advertiser` with `Disabled == false` builds Zeroconf with the subtype responder (what the reference daemon wires today — read `examples/reference-bridge/main.go`); `Disabled` is `mdns.NewNoop()`. **Before changing the nil default, check every in-repo constructor of `node.Config` (fabrictest, tests, examples): a site device must not open a real socket — fabrictest sets its own advertiser or must set `Disabled`.** `Node.DiagnoseMDNS() []MDNSFinding` (alias of `mdns.Finding`) over the node's services; `Node.MDNSEnabled() bool`. The change of the nil default is `### Changed`. |
| `endpoint.SourceKey`, `endpoint.AvailabilityProbe` (the types of `device.Options.StableKey`, `.Reachable`) | aliases `device.SourceKey`, `device.AvailabilityProbe` — the one change in `device/` this brief makes (`device/options.go`, two lines; BT does not touch that file). |
| `groups.NewManager` (`node.Config.Groups`) | nothing to build: `node.New` builds the manager when the field is nil (`node/node.go:155-157`); document the field as an advanced seam. |
| `bootid.EnableRotation` (`bootid/`, package doc: rotation disabled by default, dev use) | `node.Config.BootID.RotateUniqueIDs bool`; `node.New` calls `bootid.EnableRotation()` when set, before anything derives a UniqueID (read where `bootid` is consulted). |
| `diagevent.Event` | `type DiagnosticEvent = diagevent.Event` in `node`; the ring is phase 21. |
| `cluster/core.SoftwareVersionFromString`, `RegulatoryIndoorOutdoor` | `var SoftwareVersionFromString = core.SoftwareVersionFromString` in `node`; the regulatory location: read `node.Commissioning` — if it has no field, add `RegulatoryLocation` with a `node` type alias of core's enum and wire it where the daemon sets it today. |
| `schema.*`, `eligibility.*`, `cluster/<server>` | no change; named in the seams page as the typed surface. |

## The advanced-seams page

`docs/guides/advanced-seams.md` (English, published): one section per
package that stays public below the facade — `bridge` (`Node.Bridge()`:
the `Attach…`/`Set…` ports, with a link to `bridge/bridge.go`'s package
doc), `endpoint` (the Matter-near level for a host with its own assembler,
ADR 0018 "the existing layer stays"), `store` (a host's own store, phase
21), `mdns` (an own advertiser), `secure/*` (what a host may need and what
it must not touch), `groups`, `im/subscription`, `diagevent` — each with:
what it gives, when a host needs it, what it costs (what the facade no
longer guarantees). Link it from `docs/guides/ports.md` and
`docs/architecture.md` ("Boundaries"), add it to `mkdocs.yml` nav next to
`ports.md`. `docs/getting-started/device.md` "Configuration" gets the new
`Config` fields (Health, Store.Path, Attestation.Files, Network.MDNS,
BootID) in its table. `mkdocs build --strict` must pass (CI runs it); a
link out of `docs/` is an absolute GitHub URL.

## The reference daemon

`examples/reference-bridge/main.go` moves to the routes (advertiser by
default, credentials by `NewCommissioningCredentials` where it drew them
itself, attestation files, store path, health probe). Composition and
FeatureMap do not move, so no families run; `make chiptool-test` runs
once because the mDNS wiring moved (the harness is this wave's; BT does
not use it).

## How a port is built

- One commit per route or group of routes (`m4/phase-20a BS: <route>`),
  each with its unit test in `node/` (`node/facade_routes_test.go` or per
  topic): the store opens from `Path` and closes on `Stop`; `FabricInfos`
  carries the fields; the attestation files load (use the test chain
  files the repo has, `secure/attestation` testdata); the health probe
  samples on the fake clock; nil advertiser + `Disabled` = Noop, nil
  advertiser otherwise = Zeroconf (assert the type, do not start it in
  the test, or start on loopback only if the existing zeroconf tests do);
  the credentials validate; the errors are the bridge's (`errors.Is`).
- `// Mirrors` comments only where a matter.js behaviour is mirrored
  (the mDNS default: `ServerNode`'s network defaults — cite the line you
  read at `862ad75f`; otherwise cite nothing).
- CHANGELOG `[Unreleased]`: `### Added` per route, `### Changed` for the
  nil-advertiser default, `### Deprecated` for `Fabrics()` with its
  replacement and the removal release.
- Guards: `make ci`, `make race`, `make cover-check`, `make reachability`
  then `-check` (new exported API moves the inventory: regenerate and
  commit the snapshot in the same commit), `make cite-check`,
  `make testparity-check` (unchanged), `make chiptool-test`. Log HEAD before
  and after and one exit line per guard in the report.
- Branch `m4/phase-20a`, one pull request labelled `needs-chiptool`, no
  auto-merge. Copy this brief to `notes/briefs/2026-10-20-BS-m4-phase-20a-facade.md`
  in the first commit.

## Report shape (`notes/briefs/reports/BS.md`)

As BR: **Routes** (the table above with what was built, file:line);
**Still imported by the daemon** (package, symbol, reason); **Loom reads**
(what you read in `openccu-loom` for the open items: `LocalAddr`,
`SettingUniqueID`, the window payload, the manager types, the fabric
fields — file:line each); **Fixes** (anything you found broken on the
way, own commits); **Guards** with HEAD and exit lines, the chiptool-test
result; **Differences** to this brief; **Decisions** the owner should
check; **Left out**; **Questions**; `by_design` candidates.

## Out of scope

The typed cluster layer and the declaration (BT, BU); apidiff (BV);
`contract/`; the second store, metrics, limits (phase 21); loom's own
migration; anything inside `secure/` beyond the one call; the families
table and `docs/certifiability.md`.
