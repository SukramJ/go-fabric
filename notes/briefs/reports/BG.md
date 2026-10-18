# Report BG — milestone 2, phase 15b: the public test package `fabrictest`

Branch `m2/phase-15b-fabrictest` from `main` at 9c50e87 (brief BF merged).
matter.js read only through `git -C ../matter.js show 862ad75f:<path>`:
`packages/node/src/testing/mock-site.ts`, `mock-server-node.ts`,
`node-helpers.ts` (the files the brief names). No `// Mirrors matter.js …`
comment of production code was added or changed; the matter.js citations
in `fabrictest` name only those three files and MockTime
(`packages/testing/src/mocks/time.ts`, concept §A.4).

| Commit | Item |
| --- | --- |
| 468fac7 fabrictest — site, network, devices, controllers, pairs, wire helpers; the udp and bridge listener seams | 1, 2, 3 |
| fdbeaea the device layer and both Identify servers on the node clock | 6 |
| 26aca6f controller tests onto fabrictest | 4 |
| b916a88 guide, concurrency guide, CHANGELOG, floors, accessor and guide tests | 5 |
| d7ff97d, 57f587e lint fixes; regenerated reachability and citation artefacts | — |

## Item 1 — the in-memory network

- **Built.** `fabrictest/net.go`: `Network` — datagrams over channels, one
  socket per host at `[fe80::<index>]:5540` (index from 1, in add order),
  `NetOptions{Loss, Delay, Duplicate, Seed}` (`Delay` through the site's
  clock), `Set`, `Filter` (drop chosen datagrams), `Sent`, `Datagram`. A full
  receive queue (1024) drops, as a socket does. `fabrictest/mdns.go`: the
  site answers each controller's DNS-SD queries (its `mdns.QueryConn`) from
  every running device's advertiser — PTR, subtype PTRs, SRV, TXT, AAAA of
  the device's site address — so the commissionable record while a window
  is open and the operational record per fabric. `Options.Loopback`: real
  UDP sockets on `[::1]`.
- **Guard.** `TestNetworkLossAndDelay` (full loss fails a read once MRP runs
  out on the fake clock; delay + duplicates succeed; a filter drops one
  datagram and MRP recovers), `TestLoopbackPair`.
- **Decisions.**
  1. **Node side: not `bridgetest.ServeConn`, a listener-shaped seam.** The
     missing seam was node-*initiated* traffic: the bridge sends to a UDP
     address only through its `*udp.Listener` (`bridge/case_initiator.go:380`
     and `:427`, `bridge/ota_provider_link.go:422`, the ack pump, reports),
     and a `ServeConn` connection carries replies only; a restarted device
     re-establishing its subscription (both live tests) opens CASE to the
     controller. A conn route also behaves unlike UDP (a session never moves
     off it, it is evicted when the conn closes, `bridge/conn.go`). So the
     device's bridge runs the production `udp.Listener` over an in-memory
     socket: `internal/udpseam` (new, `PacketConn` + `NewListener`, installed
     by `transport/udp`'s init; `Listener.conn` is now that interface,
     multicast answers `ErrNoMulticastInterface` on a non-kernel socket) and
     `bridgeseam.UseListener` (new; `Bridge.openUDP`, used by
     `openTransports`, refused once started or with `TCP.Incoming`). No
     exported production API changed; `bridgetest.ServeConn` is unchanged.
  2. **Controller side: `exchange.NewManager` over the same in-memory
     listener**, the seam the BLE path plugs into (an exchange manager on a
     carrier of its own). `commissioner.Options.Channel` is not used: it marks
     a BLE commissioning (network steps, channel closed with PASE,
     `controller/commissioner/commissioner.go:111-118`).
  3. **Loopback answers DNS-SD from the site, not multicast.** "Real mDNS"
     and a loopback socket do not meet: zeroconf publishes the host's
     interface addresses, not `::1`. A test that needs multicast passes its
     own advertiser via `Topology.Configure` and its own controller (as
     `controller/commissioner/node_live_test.go` does).
  4. Link-local addresses carry no zone; the network keys sockets on
     address and port.
- **Left out.** TCP on the in-memory network (a device with
  `TCP.Incoming` fails to start on it; Loopback carries it).

## Item 2 — `fabrictest.Site`

- **Built.** `NewSite(t, Options{Clock, Net, Loopback, Settle, Logger})`, a
  `clocktest.Fake` at `DefaultStart` unless `Options.Clock`; `Clock`, `Fake`,
  `Net`, `Devices`, `Controllers`, `Close` (LIFO, idempotent, `t.Cleanup`).
  `AddDevice(Topology{Bridged, Snapshotter, Configure})` → `Device` (`Node`,
  `Bridge`, `Store`, `Endpoints`, `EndpointOf`, `Addr`, `Passcode`,
  `Discriminator`, `Advertiser`, `Act`, `Start`, `Stop`, `Restart`, `Index`).
  `AddController(ControllerOptions{FabricID (1), NodeID (112233), VendorID,
  Label, CATs, Logger})` → `Controller` (`Exchanges`, `Names`, `CA`,
  `Fabric`, `Peers`, `Resumption`, `Subscriptions`, `Commissioner`,
  `Registry`, `Addr`, `Commission`). `CommissionedPair`/`UncommissionedPair
  (PairOptions{Device, Controller, Commission})` → `Pair{Controller, Device,
  Address, Peer, Interaction}` + `Pair.Node()` (the registry's client node,
  `Registry.ForAddress`). Every device has a site resolver so it reaches a
  controller of the site (`AttachOperationalResolver`). `Site.Run` and
  `Site.Eventually` move the fake clock (below); `Site.Settle`.
- **Guard.** `fabrictest/*_test.go`, 9 tests; `-race -count=3` green;
  coverage 86.9 % (floor 85, `script/coverfloor/floors.go`; `internal/udpseam`
  floor 0, declaration-only).
- **Decisions.**
  1. **`Options.Clock` is a `clock.Clock`**, nil a new Fake: the startNode
     migrations keep their wall clock (`clock.System{}`), because their tests
     drive controller parts of their own on the system clock against the node.
     `Site.Fake()` is nil then and `Run` only runs the work.
  2. **When a fake clock moves.** Inside `Run`/`Eventually` (every helper and
     `Commission` use them): to the next deadline each time the site has been
     quiet for 10 ms — no datagram sent or queued **and no goroutine of the
     process runnable, running or in a syscall** (`runtime/metrics`
     `/sched/goroutines/*`). The first cut (network quiet only, 25 ms) failed
     under `-race`: a device still computing its response saw the
     controller's response timeout fire. The goroutine reading is the Go
     form of "MockTime moves only while no task is pending". Process-wide, so
     a parallel test's work delays a step (documented in the guide).
     Commissioning moves the fake clock by 0 s.
  3. **Store.** A SQLite file per device in `t.TempDir()` (credential and
     endpoint stores, as the four startNode copies had), not
     `endpointtest.FakeStore`: that one is concurrency-naive
     (`endpoint/endpointtest/endpointtest.go:63`) and the node's assembler
     runs concurrently. It survives `Restart` — MockSite's `storageFor`.
  4. `Restart` serves the same `Topology` (the same cluster server objects).
  5. `Act(func(*node.Node) error)` serialises with Start/Stop/Restart; a
     `Server(ep, cluster)` lookup was not added (the test holds its servers).
  6. Commission uses the device's address directly (as bridgetest and both
     live tests did); the operational CASE resolves through the site's
     DNS-SD. NodeID 0 lets the commissioner draw one (matter.js's
     `peers.commission` assigns too).
  7. `discovery.Names` stays on the wall clock (no clock seam; its TTLs of
     120/4500 s outlast a test) — said in the guide.

## Item 3 — the wire helpers

- **Built.** `fabrictest/wire.go`: `Connect` (CASE when the peer has none),
  `Read` (one attribute → `interaction.AttributeData`, an attribute status
  → `*interaction.StatusError` with Path), `ReadWith`, `Events`, `Write`,
  `WriteTimed`, `Invoke`, `InvokeTimed` (→ `interaction.Result`), `Subscribe`;
  `ErrNotCommissioned`. All run under `Site.Run`.
- **Guard.** `TestCommissionedPair`, `TestEventsAndSubscribe`,
  `TestUncommissionedPairAndRegistry`, `TestPlugToggles`.
- **Left out.** An event *status* path is untested: a concrete event read
  of an unknown endpoint came back with no EventStatus at all (observed in a
  scratch test, not investigated — not this brief's subject).

## Item 4 — migration

- **Moved, assertions intact:**
  - `controller/handshake/helpers_test.go` `startNode`, `controller/interaction/node_test.go`
    `startNode`, `controller/peer/node_test.go` `startNode` → a fabrictest
    device on Loopback with `clock.System{}`, the same listen address,
    passcode and BasicInformation via `Topology.Configure` (the handshake
    test greps the NodeLabel). The tests calling them are unchanged.
  - `controller/interaction/live_test.go` (TestLiveWrite, TestLiveSubscribe,
    TestLiveLivenessTimeout, TestLiveSustainedSurvivesRestart) and
    `controller/client/live_test.go` (TestLiveClientNode) → a
    `CommissionedPair` in memory on the site's Fake, the same fabric id,
    node ids and attestation policy. Every check kept; what changed is how
    they wait: a real-time `select`/poll became `Site.Eventually`
    (the clock must move for reports, keepalives, the persist loop); the
    node restart is `Device.Restart`; TestLiveLivenessTimeout used a
    registry-only fake clock with the node on the wall clock — now one clock,
    so the test sets `Net.Loss = 1` after subscribing (no keepalive reaches
    the controller) and keeps `waitTimer(timeout)` + `Advance(timeout)` +
    the Done check.
  - `controller/client/registry_live_test.go` uses the old helpers
    (`liveNodeAt`, `live`, `fakeMDNS`, constants); they moved into that file
    unchanged (two unused fields dropped). Its own wiring is not migrated —
    not named by the brief.
  - No Go test renamed → `script/testparity/register.json` untouched,
    `make testparity-check` green.
- **Impossible as written: `controller/commissioner`'s `startNode`.** Its
  tests are in `package commissioner` (internal); `fabrictest` imports
  `controller/commissioner`, so importing it there is an import cycle. Moving
  those five files (`node_test.go`, `signer_test.go`,
  `fabric_persist_test.go`, `node_live_test.go`, `wiring_test.go`) to
  `commissioner_test` needs exported access to unexported helpers
  (`dacKeyOf`, `rootAttr`, …). Left as is.
- **Deprecated `bridgetest`/`endpointtest` symbols: none.** No symbol is
  fully covered: `fabrictest` devices are `node.Node`s, while loom's
  `bridgetest.Dial` users dial a `bridge.Bridge` they build themselves;
  `ServeConn` (BTP), `EstablishSubscriptionTarget`, `AckPumpTick`,
  `NewEmptySnapshotter`, `NewFakeStore` have no fabrictest counterpart. A
  `Deprecated:` on `Dial` would warn loom about a case fabrictest cannot
  serve. Instead the `bridgetest` package doc points to `fabrictest` for
  `node.New` devices. All exported symbols kept.

## Item 5 — docs and CHANGELOG

- `docs/guides/testing-in-process.md` (nav "Testing in process"): first test
  (runs verbatim as `fabrictest/guide_test.go` `TestPlugToggles`), what a
  site runs, the helpers, time, network shaping, restart, sockets, limits;
  each section ends in its sources. `docs/guides/concurrency.md` names
  `fabrictest`, `device.Options.Clock`, `endpoint.Config.Clock`;
  `docs/architecture.md` test-support row; README test-support paragraph
  (`clock/clocktest`, `fabrictest`); CHANGELOG `[Unreleased]` Added
  (`fabrictest`, `device.Options.Clock`, `endpoint.Config.Clock`) and Changed
  (Identify on the node clock; the controller tests on fabrictest).
  `mkdocs build --strict` exit 0.

## Item 6 — the rest of the device side on the node clock

- **Built.** `device.Options.Clock` → the OnOff server's `TimeSource`
  (`newOnOffServer`; the factory signature takes the clock).
  `endpoint.Config.Clock` → the state registry → every bridged endpoint's
  Identify (`SetClock`). `node.New` passes `Config.Clock` to its assembler
  and to the Aggregator's Identify (`buildAggregatorClusters`). Names: the
  name `Clock` was free everywhere, so no `TimeSource`.
- **Guard.** `TestDeviceTimersOnTheSiteClock`: OnWithTimedOff on a
  device-layer light, IdentifyTime on the bridged endpoint and on the
  Aggregator end exactly when the test advances the Fake.
- **Note.** A Fake ticker drops a tick its receiver has not taken (BF's
  semantics), so the Identify countdown is stepped a second at a time; the
  guide says so.
- **Left out.** A host topology (`node.Config.Topology`) builds its own
  assembler, so its `endpoint.Config.Clock` is the host's to set.

## Guards (HEAD 57f587e)

| Guard | Result |
| --- | --- |
| `make ci` (build, vet, test, lint, cover-check, bluez-check) | exit 0 |
| `make race` | exit 0 |
| `make reachability-check` | exit 0 — regenerated: unreached stays 5 (ratchet), +32 exported, all reached |
| `make cite-check` (`MATTERJS_DIR` = the main checkout) | exit 0 — drift stays 266 (one drifted wording in `site.go` reworded), lines moved |
| `make testparity-check` | exit 0 — no test renamed |
| `make chiptool-test` (once) | exit 0: chip-tool commissions the reference bridge (15.0 s), Go controller commissions (3.3 s) and subscribes (8.8 s), chip-tool suite (10.5 s); `TestMatterJSController` SKIPPED — no built matter.js beside the worktree (environment; CI runs it) |

## Questions

1. `controller/commissioner`'s startNode: move its node tests to
   `commissioner_test` with an `export_test.go` bridge, or a device-only
   sub-package of fabrictest without controller imports — or leave it?
2. Deprecate `bridgetest.Dial` anyway (pointing node.New users to
   fabrictest), or add `Site.AddBridge(*bridge.Bridge)` first so loom's case
   is covered?
3. `controller/client/registry_live_test.go` keeps a hand-built wiring:
   migrate it in a follow-up?
4. The process-wide goroutine reading in `Site.Run` — acceptable, or should
   a step also be bounded by a per-site budget?

## by_design candidates

None. The in-memory network and the clock steps are test support; no
device or controller behaviour changed apart from both Identify servers
following `node.Config.Clock` (same intervals, same default clock).
