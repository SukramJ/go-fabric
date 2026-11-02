# W5 verdict (2026-11-01, HEAD 769ea3c2)

Theme files: `wave-5-ex.md` (Opus, done), `wave-5-api.md` (Opus, done).

## Verified against the code (Fable re-read)

| ID | Severity | Verdict |
| --- | --- | --- |
| W5-ex-01 | **high** (fabrictest / host paths with `#` or `?`) | confirmed: `node/store_path.go:30`, `controller/open.go:36`, `fabrictest/device.go:159`, `examples/quickstart-device/main.go:72` build `"file:"+path+"?…"` without escaping; a `#`/`?` in the path opens another file and drops the pragmas (foreign keys off). Go subtests named `iter#02` hit it in fabrictest. Fix: one `node`-level opener with `url.PathEscape`/`(&url.URL{Scheme:"file", Path: path, RawQuery: …}).String()`, used by all four sites. |
| W5-ex-05 | medium | confirmed: no `Chmod`/`0600` anywhere in node/, controller/, store/, examples/ — the SQLite file holding operational keys (and the controller's CA root key) is created under the umask. `docs/guides/operations.md:37` says to protect it like a key. Fix: create with 0600 (and the WAL/SHM siblings), document. |
| W5-ex-02, 03, 04 | medium | reviewer-confirmed (quickstart-device bypasses the facade; quickstart-controller uses `Nodes()[0]` instead of the paired node; reference-bridge README stale) — doc/example fixes, no API change. |
| W5-api-01 | medium (API) | confirmed: `node/commissioning.go:483` OpenBasicWindow, no Close on the Node; `node.go:977` hands out the bridge's `CommissioningWindow`. matter.js pairs allow/end (`DeviceCommissioner.ts:130, :319`). Owner decision: add `CloseWindow(ctx)` and stop exposing the bridge type (API break for callers of `CommissioningWindow()`). |
| W5-api-02 | medium (API) | confirmed: `device/options.go:40-44` probe and `device/lifecycle.go:112` SetReachable; only the latter emits ReachableChanged. matter.js has one `reachable` state (`BridgedDeviceBasicInformationServer.ts:37`). Owner decision: one input (SetReachable, probe removed or probe drives SetReachable). |
| W5-api-03 | medium (API) | confirmed: `node/node.go:164` `New(cfg)` without ctx, `:182` `context.Background()` for store open/migrate; goroutines until Stop. Owner decision: `New(ctx, cfg)` (API break) or defer the store work to Start. |
| W5-api-04, 05 | medium (API) | reviewer-confirmed: four callback shapes against ADR 0025 principle 8; `device.Logger` package global ignores `node.Config.Logger` (ADR 0026 principle 9). |

## Counts (from the reviewers' reports)

| Theme | high | medium | low | note |
| --- | --- | --- | --- | --- |
| ex (Opus) | 1 | 4 | 6 | 4 |
| api (Opus) | 0 | 5 | 12 | 3 |
| **W5** | **1** | **9** | **18** | **7** |

## Owner decisions (W5)

- 2026-11-01: loom is the only consumer, so replaced API goes at once with a migration line (README §API stability, "remove, do not deprecate"); W5-api-01/02/03 are API breaks; W5-api-03 becomes `node.New(ctx, cfg)` with the goroutine starts moved to `Start`; W5-api-02 keeps `SetReachable` as the one input and removes the probe; TimeSynchronization stays mounted by default; the `EventFiring` fields and the tracked `clustergen` binary go.
- 2026-11-02: every API finding of `wave-5-api.md` is in the one W5 PR (one break wave for the consumer); W4-ctlint-14 (clock and logger for `otbr.Probe`/`Translate`, `esphome.Discover`) is carried into it as fields of the existing option structs. The owner's reviewer files the host's follow-up issue after the merge.
- 2026-11-02: W5-api-06 — a failed `Node.Start` leaves the node startable, as matter.js `Node.start` does (`packages/node/src/node/Node.ts:113-128` at 862ad75f: target state back to "offline", the error rethrown, a later start allowed); an explicit `Stop` still makes a later `Start` `ErrStopped`. W5-ex-12 — the fabrictest seam accessors move to an internal seam in W6.

## Fixed in PR (W5, branch `review/w5`)

| ID | Commit | What |
| --- | --- | --- |
| W5-api-01 | 40c1c674 | `Node.OpenOwnWindow`, `Node.CloseWindow`; `Node.CommissioningWindow()` and the alias removed |
| W5-api-02 | b5f9c2d3 | `SetReachable` the one input; `Options.Reachable`, `HostStatement.Reachable`, `device.AvailabilityProbe` removed |
| W5-api-03 | 36811c34 | `node.New(ctx, cfg)`; engine, reapers, subtype responder start in `Start`; binding worker lazy |
| W5-api-04 | 4cf5845d | `ObserveAsync`, `OnHealthSample`/`…Async`, `OnOTAProviderStatus`/`…Async`; observer hooks return nothing |
| W5-api-05 | e966c90f | `device.Options.Logger`; `device.Logger` removed |
| W5-api-06, W5-ex-08 | 378a9975, 0b66456d | Aggregator changes after `Stop` answer `ErrStopped`; a failed `Start` leaves the node startable (every fallible step before the goroutine starts; `Run` stops after it) — 378a9975's stop-on-failure replaced by owner decision 2026-11-02 |
| W5-api-07 | c7a78abb | `node.SessionParameters`, `OTAProviderServer`, `BDXResponder`, `CommissionerControlServer`, `DiscoveryNames` |
| W5-api-08 | be73b65c | `device.WithServer`, `device.Option`, `device.TypeByID` removed |
| W5-api-09 | 7fad2075 | `PairingCodes` removed; `IncreaseConfigurationVersions(ctx, …)` with `ErrNotStarted`; `PBKDFIterations uint32` on both structs |
| W5-api-10 | dedd23bf | one opt-in idiom in `node.Config` |
| W5-api-11 | b4ac157d | harness and Wi-Fi commissioning behind `internal/nodeseam` (`fabrictest.UseWiFiCommissioning` for host tests); `bootid.EnableRotation` |
| W5-api-12 | 2de3de16 | `InitialDataVersion` unexported; built-in materialisers in `cluster/measurement` |
| W5-api-13 | 6801c78b | eligibility doc, schema ids, Compat rows in `notes/parity/controller_profiles.md` |
| W5-api-14 | 6f7c4597 | `node.AheadRows` the union; `device.AheadRows` removed |
| W5-api-15 | d61a8884 | `contract.DeviceTypeName` and its generated table removed |
| W5-api-16 | 7903302f | the doc passages (the `SetOnFabricAdded` one went with W2-api-01) |
| W5-api-17 | 5eff492c | `Config.Snapshotter`, `OTAConfig`, `surface.Provenance` |
| W5-api-18 | e0e7f7e6 | devicegen accessor functions over unexported types |
| W5-api-19 | 7c5d3066 | `Observers.OnSessionOpened`, `OnSessionClosed`, `OnSubscriptionsChanged` |
| W5-api-20 | 33650a65 | `device.Options.Surface` a value |
| W5-ex-02, -03, -13 | e44b6d50 | quickstarts on the facade, the paired node, the import guard, `Config.Names` in the test |
| W5-ex-04, -10, -11, -12 | fa402bff | reference-bridge README and doc, fabrictest doc, README `internal/` list |
| W5-ex-06 | 107f897e | PASE auto-arm under the node's lifetime |
| W5-ex-07 | 792a699c | group and binding provisioning on the generated types |
| W5-ex-09 | a53362e1 | `controllernode` takes the IPv4 preference |
| W5-ex-14 | 9d5c0e14 | app pipe `0660` |
| W5-ex-15 | bc168c4b | `--wifi-credentials` / `--thread-dataset` from `@file` or the environment |
| W2-conc-02/03 follow-up | 787ad349 | `EventFiring.Timestamp`, `.Data` removed |
| W4-ctlint-14 | 809e03ad | `otbr.Config.Clock`, `BorderRouterConfig.Logger`, `esphome.Discover(ctx, Options)` |
| housekeeping | 66e51bf2 | `clustergen` binary untracked, `/clustergen` `/devicegen` ignored |

## Open notes (W5)

- W5-ex-10, three facade gaps the reference bridge still reaches below the facade for: a listing of every endpoint with its device types (`printTopology` through `Node.Bridge`; `Node.Topology` lists the bridged devices only), a facade for the thermostat's preset persistence (`store.NewThermostatPresetStore`), and a host port on the device kit's default EnergyEvse (the charger mounts `cluster/energy.NewEnergyEvse` as `HostServer`).
- W5-ex-07, a facade candidate: a `controller/client` helper for "provision a group on these nodes" and "bind A to B with the ACL entry"; the reference controller's ~150 lines are now on the generated types but every host would still copy them.
- W5-ex-12: closed here — owner decision 2026-11-02: fabrictest's seam accessors (`Device.Bridge`, `Endpoints`, `Controller.Exchanges`, …) move to an internal seam in W6. Done in the W6 PR (commit 390a75d2, `internal/fabrictestseam`; see `wave-6-verdict.md`).
