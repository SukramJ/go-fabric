# Testing against an in-process controller

A host maps its own device model onto Matter: which data point becomes
which attribute, which product becomes which device type. That mapping is
the host's code, and the place it breaks is the wire — a controller reads
an attribute the host never fills, or a command the host routes nowhere.
[`fabrictest`](https://github.com/SukramJ/go-fabric/tree/main/fabrictest)
tests it there: it commissions a node of this module with the module's own
controller, in the test process, and lets the test read, write, invoke and
subscribe over the wire — with no chip-tool, no radio and no socket.

The package follows matter.js's testing support: `Site` is matter.js's
`MockSite`, a device exposes what `MockServerNode` exposes, the wire
helpers follow the `interaction` namespace of `node-helpers.ts`, and the
default clock is a `clocktest.Fake`, matter.js's `MockTime`.

## A first test

```go
func TestPlugToggles(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{})
	plug, err := device.NewOnOffPlugInUnit(device.Options{
		StableKey: device.StringKey("plug:1"), Name: "plug",
	})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{Bridged: []node.Endpoint{plug}},
	})
	ep, _ := plug.EndpointNumber()
	// A deadline of the test's own: a wait without one ends after the
	// site's (Options.Wait, 30 s on the wall clock) with ErrWaited.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	toggle := client.CommandAt(ep, onoffdef.ClusterID, onoffdef.CmdToggle)
	if _, err := fabrictest.Invoke(ctx, pair, toggle, nil); err != nil {
		t.Fatal(err)
	}
	values, err := fabrictest.ReadCluster(ctx, pair, ep, onoffdef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	if state, _ := onoffdef.StateFrom(values); !state.OnOff {
		t.Fatalf("OnOff %+v", state)
	}
}
```

`CommissionedPair` adds a controller and a device to the site and runs the
controller's commissioning flow against the device: PASE, the
GeneralCommissioning and OperationalCredentials commands, CASE over the
device's operational record, CommissioningComplete. The pair's `Peer` holds
the CASE session and its `Interaction` speaks over it. The site closes every
host when the test ends.

*Source: `fabrictest/controller.go` (`AddController`, `Commission`,
`CommissionedPair`), `fabrictest/device.go` (`AddDevice`, `Topology`),
`fabrictest/guide_test.go` (this test, as it runs), `fabrictest/site_test.go`,
`fabrictest/clock_test.go`.*

## What a site runs

| Part | What it is | Accessors |
| --- | --- | --- |
| Device | a `node.Node` serving the `Topology`: its `Bridged` endpoints under the node's own Aggregator, or a host `Snapshotter` (`node.Config.Topology`). Its credential and endpoint stores are in memory (`store/memstore`, `endpoint/memstore`), or with `Options.StoreOnDisk` SQLite files in the test's temporary directory; its advertiser is an `mdns.Noop` unless `Configure` sets one. Passcode 20202021, discriminator 3840 | `Node`, `Bridge`, `Store`, `Endpoints`, `EndpointOf`, `Addr`, `Advertiser`, `Act` |
| Controller | the module's controller stack: exchange manager, DNS-SD cache, certificate authority, one fabric (id 1 and node id 112233 unless `ControllerOptions` say otherwise), peer set, subscription registry, commissioner with its CASE responder, client node registry | `Exchanges`, `Names`, `CA`, `Fabric`, `Peers`, `Subscriptions`, `Commissioner`, `Registry` |
| Network | datagrams between the hosts over channels, addressed like UDP: host *n* (counted from 1 in the order the site adds them) is `[fe80::n]:5540`, *n* in hex | `Site.Net` |
| DNS-SD | the site answers each controller's queries from every running device's advertiser — the commissionable record while a window is open, an operational record per fabric — with the device's address | — |

`Topology.Configure` edits the node's configuration before `node.New`: a
passcode, a product name, an OTA requestor. `fabrictest.Endpoint(key,
deviceType, servers...)` is a bridged endpoint for `Topology.Bridged` that
serves cluster servers the test built itself, for a server under test
outside the device layer; `Device.EndpointOf(key)` finds its number. A
device-layer device states its feature selection and initial state in its
declarations (`onoffdef.Declare{…}`) and knows its own number once added
(`plug.EndpointNumber()`). Both hosts run the production
code: the device's bridge serves the production `transport/udp.Listener`
over a socket of the in-memory network, so every datagram it receives,
answers or initiates takes the path a kernel socket would.

`Pair.Node` returns the controller's client node of the device
(`client.Registry.ForAddress`): bound to the peer, neither started nor
subscribed.

*Source: `fabrictest/device.go` (`config`, `Start`), `fabrictest/endpoint.go`, `fabrictest/controller.go`
(`start`), `fabrictest/net.go`, `fabrictest/mdns.go`,
`internal/udpseam/udpseam.go`, `bridge/seam.go` (`UseListener`).*

## The wire helpers

| Helper | Request | Result |
| --- | --- | --- |
| `Connect` | a CASE session when the peer holds none | the pair's interaction |
| `Read` | one attribute, fabric-filtered | its `AttributeData` |
| `ReadCluster` | every attribute of one cluster of an endpoint | its values, for the generated `<cluster>def.StateFrom` |
| `ReadWith` | any `ReadRequest` (wildcards, events, filters) | the whole `Report` |
| `Events` | event paths | the events in report order |
| `Write`, `WriteTimed` | one attribute; timed sends a TimedRequest first | nil |
| `Invoke`, `InvokeTimed` | one command | the `Result` |
| `Subscribe` | a `SubscribeRequest` | the `Subscription`, once the device answered |
| `Exchange` | one IM message as it stands (opcode, TLV payload) — a request the controller refuses to formulate | the device's first answer |
| `Send` | the same | nothing; for a request the device answers with nothing |

Three helpers shape the pair the requests go over. `Pair.Grant(ctx,
privilege)` replaces the device's access control list on the pair's fabric
with one CASE entry for the controller at that privilege — a View or
Operate subject, as matter.js's `MockServerNode.online({ accessLevel })`.
`Controller.PASE(ctx, device)` opens a PASE session to a device in its
commissioning window and returns a `Pair` whose `Interaction` speaks over
`Pair.PASESession` — the session without fabric of a commissioner before
AddNOC; the wire helpers accept it. `Device.OpenWindow(ctx, timeout)` opens
a basic commissioning window with the device's own passcode, so a second
controller commissions it onto another fabric.

The helpers' types are `fabrictest`'s own aliases of the controller
role's (`AttributePath`, `ReadRequest`, `SubscribeRequest`, `Report`,
`Result`, `StatusError`, …), so a test names no package below the facade;
paths are built with `client.AttributeAt`, `client.ClusterAt` and
`client.CommandAt`, a scalar to write with `spec.Uint8(20)`,
`spec.Int16(2250)` or `spec.Null[uint16]()`. The typed read of a cluster
is `ReadCluster` with the generated `StateFrom`, which fills the same
`State` the device's typed server reads.

A status other than Success is an `*StatusError`: the IM status,
the cluster status when the device sent one, and the attribute path for an
attribute. An uncommissioned pair returns `fabrictest.ErrNotCommissioned`.
`ReadCluster` reads every attribute of the cluster, a wildcard path the
device skips without a status where it matches nothing: on an endpoint
the device no longer serves (removed from its Aggregator) it returns no
values and no error, so a test of a removal checks for the empty result.

*Source: `fabrictest/wire.go`, `fabrictest/raw.go`, `fabrictest/access.go`,
`fabrictest/pase.go`, `fabrictest/window.go`.*

## Client nodes

`Site.CommissionedPair` commissions with the commissioner alone; the
controller's client node registry holds nothing until a test asks
(`Pair.Node`). `Controller.CommissionNode(ctx, device, opts)` commissions as
matter.js's `controller.peers.commission` does: the device is discovered by
its long discriminator, the registry's node for it is commissioned with
`client.CommissionOptions` and started. `Controller.Discover(ctx, id,
timeout)` is the discovery alone, the timeout on the site's clock.
`ControllerOptions.Registry` edits the registry's configuration — hooks,
`AutoStart`, a store — before the registry is built, on every start;
`ControllerOptions.Attestation` sets the commissioner's trust store and
`NoAttestationStore` commissions without one. The registry itself is not
started: a test that wants `AutoStart` calls `Registry().Start`.

*Source: `fabrictest/node.go`, `fabrictest/controller.go`.*

## A program's own composition

`Site.AddController` builds the controller stack the site's way. A program
that composes its controller with `controller.New` tests that composition
instead: `Site.ControllerConfig(opts)` returns a `controller.Config` wired
to the site — a host index with its socket on the site's network
(`Listener`), the DNS-SD browser on the site's answers (`Names`), the
site's clock with the host's in-flight counters (`Clock`), the site's
logger and a SQLite database of its own in the site's directory. The test
edits the configuration as the program does, calls `controller.New` and
`Controller.Pair` under `Site.Run`, and closes the controller under
`Site.Run` before the site closes. The site's devices do not resolve such a
controller as an operational peer: a device's subscription re-establishment
and an ICD Check-In reach the controllers of `AddController` only.

```go
site := fabrictest.NewSite(t, fabrictest.Options{})
dev := site.AddDevice(fabrictest.Topology{})
cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
var c *controller.Controller
_ = site.Run(ctx, func(ctx context.Context) (err error) { c, err = controller.New(ctx, cfg); return err })
defer site.Run(ctx, c.Close)
code, _ := setup.ManualCode(dev.Discriminator(), dev.Passcode())
_ = site.Run(ctx, func(ctx context.Context) error {
	_, err := c.Pair(ctx, code, controller.PairOptions{OnAttestationFailure: attestation.Accept()})
	return err
})
```

The device side has the same route. `Site.AddNode(cfg)` takes the
`node.Config` a daemon composes for `node.New` and runs it on the site:
the site puts in its network (the listen address and, with
`Client.Enabled`, the client socket and DNS-SD cache), its clock, an
advertiser whose records it answers DNS-SD with, its logger when the
configuration has none, and a store of its own when the configuration
states none; TCP and the re-announcement are off. Everything else — the
passcode and discriminator, the product, the labels, the ICD or OTA
configuration, the host topology — is the daemon's. The device serves
`cfg.Topology` or the node's Aggregator, empty until the test adds to it
through `Device.Act`; `Device.Restart` builds the node from the same
configuration again.

*Source: `fabrictest/controller_config.go`, `fabrictest/device.go` (`AddNode`), `controller/site_test.go`, `fabrictest/addnode_test.go`.*

## Time

A site's default clock is a `clocktest.Fake` starting at
`fabrictest.DefaultStart`, and every device and controller of the site runs
on it: the device's ACK pump, MRP, subscription intervals, commissioning
window, persist loop, its cluster servers' timers — including the bridged
endpoints' and the Aggregator's Identify — and the controller's MRP, peer
connection, subscription liveness and registry. A device the host builds
with the device layer joins it when the node's Aggregator adds it, as in
the first test above.

The fake moves only when the test moves it, or inside `Site.Run` and
`Site.Eventually`, which every wire helper and `Commission` use. While the
work has not finished they advance the clock to its next deadline each time
the site has been quiet (see [The pacing model](#the-pacing-model)). A
timer the work waits on — a standalone acknowledgement, a retransmission, a
report interval — then fires; a computation in progress does not see time
move under it. `Eventually` and `Settle` wait as long as their context
allows; a context without a deadline is bounded by the site's own,
`Options.Wait` (default `fabrictest.DefaultWait`, 30 s on the wall clock),
after which they return `fabrictest.ErrWaited`, naming the work the hosts
still hold — a condition that never holds fails the test instead of
hanging it.

```go
// A change made locally reaches the subscriber once the clock passes the
// subscription's floor.
pair.Device.Act(func(*node.Node) error { return plug.OnOff().Set(onoffdef.Patch{OnOff: new(true)}) })
err := site.Eventually(ctx, "the report", func() bool { return reported() })
```

`plug.OnOff()` is the plug's typed OnOff server (`onoffdef` is
`github.com/SukramJ/go-fabric/cluster/spec/onoff`); its `Set` is the
device's own change, stored and reported.

A test that steps the clock itself calls `site.Step(ctx, d)`: it waits
until the site is quiet, advances the fake by `d` — every timer due in it
fires in deadline order, each callback awaited — and waits until the site
is quiet again (matter.js's `await MockTime.advance(ms)`). A ticker drops a
tick its receiver has not taken, as a real one does, so a countdown on a
ticker (Identify) is stepped one period at a time. A site given
`Options{Clock: clock.System{}}` runs on the wall clock, and `Run` only runs
the work.

The controller's DNS-SD cache (`discovery.Names`) keeps its record TTLs on
the wall clock; the site's answers carry 120 s and 4500 s, longer than a
test.

### The pacing model

The site is quiet once, for the settle period (`Options.Settle`, default
10 ms, on the wall clock), nothing moved, and:

- no datagram waits unread on the in-memory network;
- no host holds counted work. Each device and controller has its own
  counters, which the components it builds find on the clock the site
  hands them:

  | Counter | From | Until |
  | --- | --- | --- |
  | `udp.datagrams` | the host's socket handed a datagram to its listener | the listener's handler for it returned (or the listener dropped it) |
  | `exchange.delivered` | a client exchange queued a message for its reader | the reader's next `Next`, `Send`, `Close` or `Abort`, or the exchange's end |
  | `subscription.pending` | the subscription engine took a tick, or started a report | the tick was walked, the report returned |
  | `device.callbacks` | a host `…Async` callback was queued (device or node) | it returned |
  | `node.start`, `node.stop` | a node's `Start` or `Stop` began | it returned — but for its waits on peers (re-establishing subscriptions, the client stack's close) |
  | `node.build` | a site device's `Device.Start` began building the node | the node's own `Start` took over |

  A component counts work it accepted and finishes on its own, never a
  wait on its peer or on the clock: the peer may need the clock to move
  (a standalone acknowledgement's timer, a retransmission after a loss).
  Counted work that holds the clock for a second with nothing moving no
  longer holds it, and the site logs it — matter.js MockTime abandons a
  host operation after `MAX_HOST_ASYNC_MS` the same way. A `node.build`
  is exempt: it waits on no clock, and under the race detector on a
  loaded machine it can take longer than the second;
- no goroutine of the test process is runnable, running (but the waiting
  one) or in a system call. On an in-memory site with a fake clock this
  check delays a step by at most 50 ms after the site's last change: it
  stands in for work no counter names, and its bound keeps the goroutines
  of tests running beside this one from starving the site's clock. On
  loopback sockets, whose kernel work no counter sees, and on a wall-clock
  site it holds as long as a goroutine works.

`Device.Stop` (and `Restart`) returns once the network and every host's
counters are quiet after the node's stop, holding the clock meanwhile: the
controllers have handled the CloseSession farewells, so a request the test
sends next does not go out on a session its controller is about to learn
the device ended, and a restart under an outer `Run` does not let the
controllers' timers run while the device is down.

Two rules follow for code a site paces:

- **No wall-clock deadline in paced code.** A deadline in the device's
  stack or the client machinery is a deadline of the component's clock:
  `clock.WithTimeout(ctx, c, d)` and `clock.WithDeadline` end the context
  when `c` passes it — on the system clock they are `context.WithTimeout`
  itself, on a fake when the fake is advanced. A wall-clock deadline would
  expire on a loaded machine while the site's clock stands.
  `clock/nowalldeadline_test.go` fails the build on a
  `context.WithTimeout` or `context.WithDeadline` in non-test code of
  `bridge`, `node`, `im`, `client`, `groups`, `transport` and `mdns`
  (`bridge/bridgetest`, a wall-clock test helper by design, excepted).
- **No `Fake().Advance` inside a condition, or while the stack may be
  working.** A condition only reads; the site moves the clock. A test
  that steps the clock uses `Step`, `Run` or `Eventually`.

`make stress PKG=./node TESTS='^TestFoo$'` runs a test under the race
detector in `STRESS_P` (8) processes at once, `STRESS_COUNT` (50) times,
through `golang.org/x/tools/cmd/stress`; `STRESS_INPROC` runs that many
instances side by side in each process. `make stress-flakes` runs the
tests the flake sweep fixed, and the nightly workflow runs it.

*Source: `fabrictest/site.go` (`Run`, `Eventually`, `Settle`, `Step`,
`quiet`, `busy`), `internal/inflight`, `clock/context.go` (`WithTimeout`),
`node/config.go` (`Config.Clock`), `device/queue.go` (`deviceClock`),
`endpoint/assembler.go` (`Config.Clock`), `node/root.go`
(`buildAggregatorClusters`), `client/discovery/names.go` (`newNames`).*

## Shaping the network

```go
site.Net().Set(fabrictest.NetOptions{Loss: 0.2, Delay: 50 * time.Millisecond, Duplicate: 0.1, Seed: 7})
site.Net().Filter(func(d fabrictest.Datagram) bool { return !dropThisOne(d) })
```

`Loss` and `Duplicate` are probabilities, drawn from `Seed`; `Delay` is time
in flight on the site's clock, so a delayed datagram arrives when the clock
passes it. A filter sees every datagram first and drops it by returning
false. With every datagram lost, a read fails once the controller's
retransmissions run out — on the fake clock, so the test does not wait the
retransmission budget in real time.

*Source: `fabrictest/net.go` (`NetOptions`, `Set`, `Filter`, `send`),
`fabrictest/site_test.go` (`TestNetworkLossAndDelay`).*

## Restart

`Device.Restart` stops the node and starts a new one on the same store and
address, serving the same `Topology` (the same cluster servers). What the
node persisted survives: fabrics, ACL, subscriptions, endpoint numbers. A
device re-establishing a persisted subscription opens CASE to the
controller, which the site resolves for it, and the controller's CASE
responder adopts the session.

`Controller.Restart` stops the controller's stack and starts it again on its
stores — the certificate authority, the fabric, the CASE resumption records
and the client node registry's store — so the registry restores its nodes,
as matter.js's controller restarted on its storage. A pair's `Peer` and
`Interaction` belong to the stopped stack; after a restart a test uses the
registry's nodes. On loopback sockets the controller gets a new port.

*Source: `fabrictest/device.go` (`Restart`, `siteResolver`),
`fabrictest/controller.go` (`Restart`),
`client/interaction/live_test.go` (`TestLiveSustainedSurvivesRestart`),
`controller/client/live_test.go` (`TestLiveClientNode`).*

## Sockets

`Options{Loopback: true}` puts every host on a real UDP socket on `[::1]`
with an ephemeral port; the site still answers DNS-SD from the devices'
advertisers. A test that drives controller parts of its own against a device
of the site — its own exchange manager, a peer set — uses it, often with the
wall clock; `Topology.Configure` may set the device's listen address
(`node.Network.Listen`).

## Limits

- The in-memory network carries UDP only. A device configured to accept TCP
  (`node.Network.TCP.Incoming`) fails to start on it; use `Loopback`.
- A device is a `node.Node`. A host that builds a `bridge.Bridge` itself
  tests it with
  [`bridgetest.Dial`](https://github.com/SukramJ/go-fabric/blob/main/bridge/bridgetest/commissioner.go),
  which commissions over loopback UDP on the wall clock.
- BLE commissioning has its own in-memory pair:
  `controller/ble/bletest` with `bridgetest.ServeConn`.
- `Run` reads goroutine states for the whole test process, so the work of a
  parallel test in the same binary delays a clock step — by at most 50 ms
  on an in-memory site with a fake clock, until it is idle on loopback.
- Devices carry the CSA test attestation chain, the controllers trust the
  CSA test PAAs, and every credential is fixed and public: the package is
  for tests.
