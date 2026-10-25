# Getting started: the device side

This page builds a Matter bridge with one on/off light, pairs it, and walks
through what each part of the program does. The program is
`examples/quickstart-device/main.go`; its test builds it, starts the node on
loopback, stops it and starts it again on the same database.

## `node/` or `bridge/`?

Embed **`node/`** unless you have a reason not to. `node.New` performs the
whole bring-up the reference daemon used to do by hand: the stores, the root
and Aggregator endpoints, PASE and CASE, the subscription engine, the
commissioning window, the fabric hooks, and every port of `bridge/` in its
documented order. You configure it with one `node.Config` and add devices to
its Aggregator.

Use the facade with your own topology when you already have an endpoint
assembler over your own device model: set `Config.Topology` to your
`bridge.Snapshotter`. The facade then builds no Aggregator, and everything
else (stores, root servers, sessions, group state) stays the facade's.

Use **`bridge/` directly** only if you need to replace one of the
collaborators the facade wires. `bridge.New` installs a no-op for every port
you do not attach, so a half-wired bridge binds its socket, publishes mDNS
and answers datagrams wrongly. The [ports guide](../guides/ports.md) lists
every port, what skipping it costs, and whether the failure is visible.
`Node.Bridge()` gives you the bridge for a port the facade does not cover
without leaving the facade.

*Source: `node/doc.go:4-43`, `node/config.go:107-116`, `bridge/bridge.go:29-47`, `node/node.go:790-792`.*

## The program

<!-- program: examples/quickstart-device/main.go -->
```go
// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command quickstart-device is the smallest Matter bridge on go-fabric: one
// on/off light under the node's Aggregator, its state in one SQLite file,
// advertised over DNS-SD. docs/getting-started/device.md walks through it.
// It pairs with the CSA test identity and is not a product.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"

	_ "modernc.org/sqlite" // the host picks the SQLite driver

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	db, err := openDB("quickstart-device.db")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// DNS-SD: the subtype responder answers the discriminator-filtered
	// browse every commissioner sends for a QR or manual code.
	zc := mdns.NewZeroconf()
	if r, err := mdns.NewSubtypeResponder(slog.Default()); err == nil {
		r.Start(ctx)
		zc.AttachSubtypeResponder(r)
		defer func() { _ = r.Close() }()
	}

	n, err := newNode(ctx, db, node.Network{}, zc)
	if err != nil {
		return err
	}
	qr, manual, err := n.PairingCodes()
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return err
	}
	fmt.Printf("QR code: %s\nManual code: %s\n", qr, manual)
	return n.Run(ctx) // Start, wait for ctx, Stop
}

// openDB opens the SQLite file; foreign_keys carries the store's
// cascade-delete on fabric removal. node.New applies the schemas.
func openDB(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
}

// newNode builds the node and adds the light, unstarted.
func newNode(ctx context.Context, db *sql.DB, network node.Network, adv mdns.Advertiser) (*node.Node, error) {
	n, err := node.New(node.Config{ //nolint:contextcheck // the node's constructor takes no context
		Network: network,
		Commissioning: node.Commissioning{ // change all four for anything real
			Passcode: 20202021, Discriminator: 3840,
			PBKDFIterations: 1000, Salt: []byte("go-fabric-quickstart"),
		},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001}, // CSA test vendor
		BasicInformation: node.BasicInformation{
			VendorName: "quickstart", ProductName: "quickstart-device",
			NodeLabel: "Quickstart bridge", SerialNumber: "QUICKSTART-0001",
		},
		Store:      node.Store{DB: db},
		Advertiser: adv,
	})
	if err != nil {
		return nil, err
	}
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light-1"), Name: "Desk lamp"})
	if err == nil {
		light.OnChanged(onoff.ClusterID, func(c contract.Change) {
			if slices.Contains(c.Attributes, onoff.AttrOnOff) {
				slog.Info("light", slog.Bool("on", light.OnOff().Get().OnOff)) // drive the real lamp here
			}
		})
		err = n.Aggregator().Add(ctx, light)
	}
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return nil, err
	}
	n.Aggregator().MarkComplete() //nolint:contextcheck // the node's own; every device is added: stale endpoint numbers may go
	return n, nil
}
```

Run it, scan the QR code or enter the manual code in a commissioner, and the
light appears under the bridge:

```sh
go run ./examples/quickstart-device
```

It listens on UDP port 5540 and needs IPv6 and multicast on the LAN, like
any Matter device. A commissioner that filters by discriminator, which is
what Apple Home, Google Home and chip-tool do for a QR or manual code, finds
the device only through the subtype responder. Without the responder,
`_matterc._udp` lists the instance, but the discriminator subtype query
gets no answer.

*Source: `examples/quickstart-device/main.go`, `examples/quickstart-device/main_test.go`, `examples/reference-bridge/main.go:170-174`, `node/config.go:210-211`.*

## Configuration

`node.New` calls `Config.Validate` first and refuses the whole configuration
with `node.ErrInvalidConfig` when any check fails. It checks:

- the discriminator fits 12 bits;
- the passcode is in range and not on the trivially-guessable list;
- the PBKDF iteration count and salt length are what SPAKE2+ accepts;
- `VendorID`, `ProductID` and `NodeLabel` are set;
- a store is given (see below), and not both `Store.Path` and `Store.DB`;
- `Attestation.Files` is not combined with `Chain` or the CD, and
  `BlockCommissioningOnError` comes with `Files`;
- `Health.Interval` is not negative;
- `SupportedFabrics` is at most 254;
- `UniqueID` is at most 32 bytes;
- `LocationCapability` is a valid regulatory location;
- `Network.ReannounceInterval` is not negative;
- `OTA.Dir` is set when `OTA` is.

The quickstart uses the CSA test vendor `0xFFF1`. With no `Attestation` set,
the node serves the CSA test attestation chain and test Certification
Declaration for the configured vendor and product. Every commissioner
accepts that chain through its test PAA, and a product must not ship with
it. A product sets `Config.Attestation` to its own DAC, PAI and CD.

Defaults worth knowing:

| Field | Zero value means |
| --- | --- |
| `Network.Listen` | `:5540` |
| `Advertiser` | the node's own `mdns.Zeroconf` with the subtype responder; `Network.MDNS.Disabled` makes it `mdns.NewNoop()`, and nothing can discover the node |
| `Network.MDNS` | Zeroconf with the subtype responder, SRV target from the interface's MAC; `Disabled` publishes nothing, `HostName` sets the SRV target |
| `Store.Path` | unused; set, the node opens that SQLite file (modernc.org/sqlite, foreign keys on, 5 s busy timeout) and `Stop` closes it |
| `Attestation.Files` | unused; set, `New` loads the vendor chain and fails when it does not load (files naming nothing keep the test chain); with `BlockCommissioningOnError` it runs on the test chain instead, commissioning refused, the error in `Node.AttestationError` |
| `Health` | no probe; `Interval` (0 is 30 s) or `OnSample` runs it from `Start` to `Stop`, `Node.Health` returns the last verdict |
| `BootID.RotateUniqueIDs` | stable UniqueIDs; set, every UniqueID rotates per process (development only, process-wide) |
| `DeviceTypeValidation` | matter.js's warn mode |
| `SupportedFabrics` | 5 (matter.js defaults to 254) |
| `ProductDescription.Name` | `BasicInformation.NodeLabel` |
| `ProductDescription.DeviceType` | RootNode (0x0016) |
| `BasicInformation.UniqueID` | derived on the first boot and pinned in the store |
| `Locales` | `en-US` alone |
| `Logger` | `slog.Default()` |

*Source: `node/config.go:37-205`, `node/config.go:303-365`, `node/config.go:466-524`, `node/node.go:131-148`.*

## Devices

The `device/` package has one constructor per Matter device type in scope:
`device.NewOnOffLight`, `device.NewDimmableLight`, `device.NewThermostat`
and so on. They are generated from the matter.js model. A constructor
mounts every server cluster the device type mandates, with a default server:
the module's own server where one runs on its own state — OnOff,
LevelControl, ColorControl, Binding, EcosystemInformation, the alarm,
BooleanStateConfiguration, ThermostatUserInterfaceConfiguration and energy
servers, and Switch, DoorLock, WindowCovering, Thermostat, the filter
monitors and LaundryWasherControls for a feature selection their server
serves (`device/defaults.go` lists them) — otherwise the generated
`spec.Server`, whose commands are unimplemented until you handle them. A `NewDimmableLight` therefore dims:
a controller's MoveToLevel moves its level, and the "with On/Off" commands
switch its OnOff.
A generated server starts where matter.js's state starts: every attribute
it lists holds the type's default, the host's `Initial`, or the fallback
matter.js's `SelectDefaultValue` gives — null when nullable, 0 for a
number or bitmap, false, an empty list — and what matter.js's default
server sets in its `initialize` (FanControl's FanMode Off, SmokeCoAlarm's
alarm states Normal, OccupancySensing's sensor type from its features). A
TemperatureSensor therefore answers MeasuredValue null until you set a
reading. An enum, a string or a struct has no fallback in matter.js either
and stays yours to state (the test `TestEveryListedAttributeHasAValue`
names them).
The endpoint assembler mounts Descriptor, Identify,
BridgedDeviceBasicInformation, Groups and ScenesManagement; the device
layer never does. To make the device blink, declare
`identifydef.Declare{HandleIdentify: …, HandleTriggerEffect: …}` — the
assembler's Identify server runs your handler before it counts
IdentifyTime, and an error refuses the command — or install them at run
time with `light.Identify().HandleIdentify(fn)`. An Identify declaration
takes these two handlers only; the assembler keeps IdentifyTime.

`device.Options` identifies the device:

- `StableKey` is required and unique. The assembler keeps the endpoint
  number under it, so the same key gets the same endpoint number after a
  restart. The quickstart's test checks this.
- `Name` becomes the bridged device's NodeLabel. `Vendor` is its
  manufacturer.
- `Reachable` is a probe for the device's reachability; nil means always
  reachable.

Declarations passed after `Options` change what the device serves. A
cluster is declared with `<cluster>def.Declare` of its generated package
(`github.com/SukramJ/go-fabric/cluster/spec/<cluster>`, imported as
`<cluster>def`), the one form for everything about it:

| `Declare` field | Effect |
| --- | --- |
| `Initial` | initial state over the device type's defaults; an optional attribute with a value is served |
| `Features` | the cluster's feature selection (zero is the device type's) |
| `OnWrite` | a veto on a controller's write |
| `Handle<Command>` | your handler in place of the server's own (an optional command is then accepted) |
| a host port (`Sink`, `LevelSource`, `Mover`, …) | the device the kit's server drives |
| `HostServer` | your own server for the cluster, replacing the kit's |

The declaration is construction; the typed server's setters of the same
name (`Server.OnWrite`, `Server.Handle<Command>`) are the change at run
time, for a host that installs or removes a hook after the device runs.

Declaring an optional cluster of the device type mounts it, even an empty
`<cluster>def.Declare{}`; Groups and ScenesManagement are then the
assembler's. A sensor's measurement cluster is declared the same way —
`temperaturemeasurementdef.Declare{Initial: …}` — and your reading reaches
it through the accessor's `Set`. Which clusters each device type mandates,
with the features and attribute constraints it requires, is listed in the
[device-type reference](../reference/device-types.md); `go doc
github.com/SukramJ/go-fabric/device` lists every typed cluster package.

The device's other options:

| Option | Effect |
| --- | --- |
| `WithServer(srv)` | your own server for a cluster without a generated package (a manufacturer-specific one); a cluster with a package takes its `Declare` |
| `HostServer(srv)` | your own server for any cluster, when your servers are a list: the cluster's `Declare{HostServer: srv}` where it has a package, `WithServer`'s mount where it has none; a port, an `Initial` or a feature selection still takes the typed `Declare` |
| `Measurement(src)` | the clusters a classified host source materialises (`contract.MeasurementSource`, one class per data point; a battery rides as the device's PowerSource), for a host whose sensors are data rather than per-type code |
| `WithChangeNotifier(n)` | tells the bridge your model changed state its servers read |
| `WithClientClusters(ids...)` | client clusters beyond the mandatory ones |

### Two directions of state

A device's state moves both ways, and the two directions use different
calls:

- **Controller to device.** `Device.OnChanged(clusterID, fn)` calls `fn`
  with the change — the attribute ids and its origin — and the typed
  `light.OnOff().OnChanged(fn)` with the new state and the change.
  `OnChangedAsync` is the same, run on the device's own goroutine instead
  of the caller's. The quickstart uses `OnChanged` to log the light's new
  state. Both fire for every change, whoever made it; `change.Origin`
  tells your own `Set` (`contract.OriginHostSet`) from a controller's
  attribute write (`OriginControllerWrite`) and a command
  (`OriginCommand`). Only a controller reaches the declaration's three
  hooks: `OnWrite`, a veto on a controller's write; `Handle<Command>`
  (`HandleOn`, `HandleToggle`, …), your handler in place of the server's
  own; and a host port, the device the server drives. To refuse a command,
  for example when the physical lamp did not switch, declare the light's
  switch: `onoffdef.Declare{Sink: relay}`. An error from the sink answers
  FAILURE and stores nothing. A hook, a port or a host server that wants
  another status returns (or wraps) a `contract.StatusError{Status: …,
  ClusterStatus: …}`: the controller receives that status, as matter.js
  answers a `StatusResponseError` — `contract.StatusTimeout` for an
  answer the device could not give in time, `0x87` for a value it refuses,
  `0x01` with a cluster status for a cluster-specific one. A status error
  naming Success answers FAILURE.
- **Device to controller.** When the lamp is switched at the wall, call
  `light.OnOff().Set(onoffdef.Patch{OnOff: new(on)})` (`onoffdef` is
  `github.com/SukramJ/go-fabric/cluster/spec/onoff`). The value is stored
  and reported to subscribers, and the sink is not called. An attribute
  Matter marks quieter — LevelControl's `CurrentLevel`, ColorControl's
  current colour — is reported at most once a second with its latest
  value, at once from or to null, as matter.js throttles it; a read and
  `OnChanged` see every change at once. `Set` is safe from any goroutine,
  including from inside the sink.

*Source: `device/doc_gen.go`, `device/defaults.go`, `device/options.go` (`Options`, `WithServer`, `HostServer`, `WithChangeNotifier`, `WithClientClusters`), `device/hostserver_gen.go`, `device/measurement.go` (`Measurement`), `device/device.go`, `cluster/spec/onoff/server_gen.go` (`Declare`, `OnChanged`, `OnChangedAsync`), `contract/` (`Change`, `Origin`), `cluster/onoff/server.go` (`Set`, `SetAttributes`, `Handle`).*

## The Aggregator

`Node.Aggregator()` is the Aggregator endpoint's set of bridged devices.

- **Before `Start`**, `Add` and `Remove` only change the set. The first
  assembly happens at `Start`.
- **After `Start`**, each `Add` and `Remove` reassembles the running
  bridge.
- An endpoint without a `StableKey` is refused with `ErrNoStableKey`. A
  second endpoint with the same key is refused with `ErrDuplicateKey`.

Call `MarkComplete` once every device you know about is added. Until then
the node keeps the stored endpoint numbers of devices that are absent.
Afterwards, a stored device missing from the set loses its number. A host
whose model loads asynchronously calls `MarkComplete` after the load. If it
is called too early, the endpoint numbers of devices not yet loaded are
garbage-collected at boot, and every controller sees those devices as new.

Once a device is added, it has a lifecycle of its own:
`Device.SetReachable(ctx, false)` reports it unreachable (the bridged
device's Reachable attribute and its ReachableChanged event),
`Device.Rename(ctx, name)` changes the NodeLabel a controller sees, and
`Device.EndpointNumber` returns the endpoint the node gave it.
`Node.Topology()` is what the node serves: the Aggregator's endpoint and
every bridged device with its key, endpoint, device type, name and
reachability. The [host's view](../guides/host-view.md) walks through a
program that uses each.

*Source: `node/aggregator.go:40-132`, `device/lifecycle.go`, `node/` (`Topology`, `BridgeTopology`), `examples/reference-bridge/main.go:329-331`, `examples/reference-bridge/fleet_node.go` (`endpointByKey`).*

## Commissioning

`Node.PairingCodes` returns the QR payload and the 11-digit manual code for
the configured passcode and discriminator. When `Start` loads no fabric from
the store, it opens the node's own commissioning window. A commissioned node opens
a window only when a controller asks for one through
AdministratorCommissioning, or when you call `Node.OpenBasicWindow` with
fresh credentials.

`Node.SetCommissioningBlocked(ctx, err)` refuses every new commissioning
while `err` is non-nil, and controllers that are already paired keep
working. A host calls it before `Start` when, for example, it cannot serve
its configured attestation chain.

*Source: `node/node.go:900-907`, `node/node.go:692-711`, `node/commissioning.go:424-452`, `node/node.go:835-855`.*

## Persistence

`Config.Store.Path` names a SQLite file the node opens itself with
`modernc.org/sqlite`, the pure-Go driver the module already depends on, and
closes on `Stop` — what the reference daemon does. `Config.Store.DB` is a
`*sql.DB` that you open instead, with a driver of your choice; the store
packages import none. `node.New` applies both schemas (fabrics,
credentials, ACL, groups, subscriptions, settings, and endpoint numbers) to
the database on every boot; both scripts are idempotent.

Opening a handle yourself, put `_pragma=foreign_keys(1)` in the DSN. The fabric
tables cascade-delete through foreign keys, and SQLite ignores those
silently with the pragma off. The pragma must be in the DSN, not in a
statement, so that every pooled connection gets it.

The [persistence guide](../guides/persistence.md) lists every table and
settings key.

*Source: `node/config.go` (`Store`), `node/store_path.go`, `node/node.go` (`openStores`, `closeOwnedDB`).*

## Stopping

`Node.Run(ctx)` starts the node, waits for `ctx` to end, and stops it with a
5-second budget. `Node.Stop` runs the shutdown in a fixed order:

1. the OTA requestor stops;
2. the operational time is stored and the ShutDown event is emitted;
3. the bindings and client stack close;
4. PASE, the commissioning window and the CASE reaper stop;
5. the subscription engine, the group sender, BDX, the re-announce
   loop and the health probe stop;
6. the bridge stops (sessions are closed with a CloseSession to each
   peer, then the socket closes and the mDNS records are withdrawn);
7. the advertiser closes, and the store the node opened for
   `Store.Path`.

`Stop` is idempotent and safe without `Start`. A stopped node does not
restart: `Start` after `Stop` returns `node.ErrStopped`. To restart, build a
new node on the same store.

`New` already starts the subscription engine and the session reapers. Call
`Stop` on every node that `New` returned, including one you never started.
The quickstart does this on its error paths.

*Source: `node/node.go:35-37`, `node/node.go:715-788`, `node/node.go:116-130`, `bridge/bridge.go:1695-1710`.*

## What the reference daemon adds

`examples/reference-bridge` is the facade plus the host's own pieces, each
of which is something a product would also decide:

- **Flags** for the listen address, TCP, passcode, discriminator, PBKDF
  parameters, label and log level. Each flag is checked before the node is
  built.
- **A vendor attestation chain** read from a directory instead of the test
  chain (`Attestation.Files`).
- **The OTA requestor** with an applier that validates and keeps the image.
  This is the host's `OtaApplier`.
- **An Actions server** on the Aggregator, root FixedLabels and two
  locales.
- **A fleet of simulated devices**, one per surface the module serves,
  each built from its clusters' declarations (`<cluster>def.Declare`): the
  initial state, the features, and the ports the simulated device
  implements — a valve's `StateSource`, a washer's `CommandHandler`, a
  lock's `AuthStore` on the node's store. The one cluster still mounted
  as a `HostServer` is the charger's EnergyEvse, whose delegate is not a
  declared port yet. The blind's position is persisted in the settings
  table.
- **Testing-only flags** for certification runs: the app pipe, the
  TestEventTrigger key, the OTA provider and its ledger check, the
  simulated Wi-Fi link and the media devices. None of these belongs in a
  product.

*Source: `examples/reference-bridge/main.go` (`parseFlags`, `checkFlags`, `run`, `nodeConfig`), `examples/reference-bridge/attestation.go`, `examples/reference-bridge/persist.go`.*
