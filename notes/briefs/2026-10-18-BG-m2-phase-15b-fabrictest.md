# Brief BG — milestone 2, phase 15b: the public test package `fabrictest`

Owner: Fable. Implementer: Opus. Concept: `docs/concept-v4-test-foundation.md`
§A.4 (owner decisions 0.7–0.8) and Teil B. Base: `origin/main` after brief
BF merged (`clock`, `clocktest.Fake`, every time source behind the port).
Branch `m2/phase-15b-fabrictest`, one PR against `main`, report
`notes/briefs/reports/BG.md`. `CHANGELOG.md` under `[Unreleased]` is yours;
`notes/parity/`, `docs/adr/`, `CLAUDE.md` are the owner's.

Read `CLAUDE.md`, `notes/briefs/README.md`, the concept section, the
inventory `notes/reference/read-test-helpers-for-fabrictest.md` §2 (the
harnesses and the four `startNode` copies, with lines), §3 (the matter.js
counterparts) and §4 (what loom imports — those symbols do not move),
`bridge/bridgetest` (`commissioner.go`, `conn.go` `ServeConn`, the package
doc), `controller/client/live_test.go` and `controller/interaction/live_test.go`
(the two hand-built wirings you replace), and at `862ad75f` via
`git -C ../matter.js show`: `packages/node/src/testing/mock-site.ts:31-230`,
`mock-server-node.ts:102-348`, `node-helpers.ts:181-520` (`CommissioningHelper`,
`namespace interaction`: connect :370, write :381, read :400, invoke :427,
subscribe :465, `seedPeerCache` :520).

## Items

1. **The in-memory network.** `fabrictest/net.go`: datagram delivery between
   the site's nodes over channels, addressed like UDP (IPv6 link-local
   addresses the site hands out, one per node), with `Options.Net.Loss`,
   `Options.Net.Delay` (through the site's clock, so MRP tests step it) and
   duplication; the node side is served through `bridgetest.ServeConn`
   (extend it only if a listener-shaped seam is missing — say which), the
   controller side through the transport seam the commissioner already has
   for BLE channels (`controller/exchange`, `controller/commissioner.Options.Channel`
   — read how the BLE path plugs in and plug in the same way). mDNS is
   answered by the site (an operational and a commissionable record per
   node) as the two live tests inject DNS answers today; `Options.Loopback`
   switches to real UDP and real mDNS for the tests that need sockets.
2. **`fabrictest.Site`.** `NewSite(t, Options)` with a `clocktest.Fake`
   (`Options.Clock` to supply one), the network, a fake store per node;
   `AddDevice(Topology)` builds an in-process node (`node.New`, the host
   snapshotter from `endpointtest`), `AddController(Options)` an in-process
   controller (CA, fabric, registry) on the same clock, `CommissionedPair()`
   both with the controller's fabric on the device and a CASE session
   (`MockSite.addCommissionedPair` :156), `UncommissionedPair()` (:144);
   `t.Cleanup` closes everything (`close` :182). Every node and controller
   exposes what `MockServerNode` exposes for a test: the node's `Bridge`,
   `Store`, endpoints, `Act(func(...))` for local server access
   (`mock-server-node.ts` `act`), the controller's `Registry` and `Node`.
3. **The wire helpers.** `fabrictest.Read/Write/Invoke/Subscribe/Connect`
   after `node-helpers.ts` `interaction` — a test speaks to the device over
   the wire through the site's controller, with typed results and the IM
   status on failure, plus `Events` and `Timed` variants the Go side needs.
4. **Migration.** The four `startNode` copies under `controller/`
   (`handshake`, `interaction`, `peer`, `commissioner` helpers) and the two
   hand-built wirings move onto `fabrictest`; `bridge/bridgetest` and
   `endpoint/endpointtest` keep every exported symbol (loom's nine), with a
   `Deprecated:` pointer where `fabrictest` now covers the case. No test
   loses an assertion; the register entries of renamed Go tests are updated
   in `script/testparity/register.json` and the page re-rendered (no status
   changes).
5. **Docs and CHANGELOG.** `docs/guides/testing-in-process.md` (or the
   existing testing page: grep `docs/` for `bridgetest`): how a consumer
   tests its device mapping against an in-process controller with no
   chip-tool and no radio, every claim checkable; the concurrency guide
   names the package; `mkdocs.yml` nav; CHANGELOG Added.

6. **The rest of the device side on the node clock** (from report BF,
   decision 4 of the owner): `device.New` builds its servers (`onoff.New`
   and the others with a `TimeSource`/`Clock` field) without a clock, and
   the bridged endpoints' and the Aggregator's Identify servers stay on
   `clock.System`. Thread `node.Config.Clock` through the device options and
   the endpoint registry so that a `fabrictest` device runs entirely on the
   site's `clocktest.Fake` (an Identify timer or a timed-off that fires on
   the wall clock would make a stepped test flaky). Field names: `Clock`
   where the name is free, `TimeSource` where an old `Clock` field holds the
   deprecated shape (the BF convention; the rename back to `Clock` happens
   when the deprecated fields go, v0.10.0 at the earliest).

## Guards

`make ci`, `make race`, `make reachability-check`, `make cite-check`,
`make testparity-check`, `make chiptool-test` once (nothing in the daemon
moves, but the node start path is shared — say if you skipped it and why).
Log every guard with HEAD before and after.

## Report (fixed shape)

Per item: built, guard, decisions, left out, questions; the list of
deprecated `bridgetest`/`endpointtest` symbols; which tests moved and that
their assertions are intact; the guide's path; by_design candidates
(expected: none).
