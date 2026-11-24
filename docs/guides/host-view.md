# A host's view

This page is a whole bridge as the host that embeds go-fabric writes it:
a dimmable light and a door contact, declared once, served by a node, with
the changes a controller makes told apart from the host's own. The program
imports `node`, `device`, `contract` and three typed cluster packages —
nothing below the facade. It is `examples/host-view/main.go`; its test
commissions both devices on an in-process controller, reads the door and
toggles the light.

## The program

<!-- program: examples/host-view/main.go -->
```go
// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command host-view is a Matter bridge as a host sees go-fabric: two
// devices declared once, a node, the changes a controller makes told
// apart from the host's own. docs/guides/host-view.md explains every
// line. It pairs with the CSA test identity and is not a product.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	booleanstatedef "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
	levelcontroldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	onoffdef "thingfabric.dev/go-fabric/cluster/spec/onoff"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/node"
)

// key is the host's stable key of a device: the device keeps its endpoint
// number under it across restarts.
type key string

func (k key) String() string { return string(k) }

// bridge is the host's two devices.
type bridge struct {
	light *device.DimmableLight
	door  *device.ContactSensor
}

// newBridge declares the devices. switched hears every change of the
// light's OnOff that a controller made — not the host's own.
func newBridge(switched func(on bool)) (*bridge, error) {
	light, err := device.NewDimmableLight(device.Options{StableKey: key("light"), Name: "Kitchen light"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(false)}},
		levelcontroldef.Declare{Initial: levelcontroldef.Patch{CurrentLevel: new(uint8(128))}})
	if err != nil {
		return nil, err
	}
	door, err := device.NewContactSensor(device.Options{StableKey: key("door"), Name: "Front door"},
		booleanstatedef.Declare{Initial: booleanstatedef.Patch{StateValue: new(true)}})
	if err != nil {
		return nil, err
	}
	light.OnOff().OnChanged(func(s onoffdef.State, c contract.Change) {
		if c.Origin != contract.OriginHostSet {
			switched(s.OnOff) // drive the real lamp here
		}
	})
	return &bridge{light: light, door: door}, nil
}

// doorMoved reports the door sensor's new state: the host's own change.
func (b *bridge) doorMoved(closed bool) error {
	return b.door.BooleanState().Set(booleanstatedef.Patch{StateValue: &closed})
}

// newNode builds the node with both devices under its Aggregator.
func newNode(ctx context.Context, cfg node.Config, b *bridge) (*node.Node, error) {
	n, err := node.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	for _, d := range []node.Endpoint{b.light, b.door} {
		if err := n.Aggregator().Add(ctx, d); err != nil {
			_ = n.Stop(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	return n, nil
}

// randomSalt is a SPAKE2+ salt drawn at random at each start, never a
// constant every copy of this example shares (R2-P6-sec-2615): the salt
// reaches a commissioner inside PASE only, so nothing needs it kept.
func randomSalt() []byte {
	salt := make([]byte, node.PBKDFSaltMax)
	_, _ = rand.Read(salt) // crypto/rand.Read does not fail
	return salt
}

// config is the node's identity and its SQLite file; change the
// commissioning values for anything real.
func config(path string) node.Config {
	return node.Config{
		Commissioning: node.Commissioning{
			Passcode: 20202021, Discriminator: 3840,
			PBKDFIterations: 1000, Salt: randomSalt(),
		},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8002}, // CSA test vendor
		BasicInformation: node.BasicInformation{
			VendorName: "host-view", ProductName: "host-view",
			NodeLabel: "Host view bridge", SerialNumber: "HOSTVIEW-0001",
		},
		Store: node.Store{Path: path},
	}
}

func run(ctx context.Context) error {
	b, err := newBridge(func(on bool) { slog.Info("a controller switched the light", slog.Bool("on", on)) })
	if err != nil {
		return err
	}
	n, err := newNode(ctx, config("host-view.db"), b)
	if err != nil {
		return err
	}
	if err := n.Start(ctx); err != nil {
		_ = n.Stop(context.WithoutCancel(ctx)) // a failed Start leaves the node startable and its store open
		return err
	}
	devices := n.Topology().Devices
	for i := range devices {
		slog.Info("bridged", slog.String("name", devices[i].Name), slog.Int("endpoint", int(devices[i].Endpoint)))
	}
	if err := errors.Join(b.light.Rename(ctx, "Kitchen ceiling"), b.door.SetReachable(ctx, true)); err != nil {
		slog.Warn("host statement", slog.Any("err", err))
	}
	if err := b.doorMoved(false); err != nil {
		slog.Warn("door", slog.Any("err", err))
	}
	payload, err := n.SetupPayload()
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return err
	}
	fmt.Printf("QR code: %s\nManual code: %s\n", payload.QR, payload.Manual)
	<-ctx.Done()
	return n.Stop(context.WithoutCancel(ctx))
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

```sh
go run ./examples/host-view
```

It pairs like the [device quickstart](../getting-started/device.md): UDP
port 5540, IPv6 and multicast on the LAN, the CSA test vendor and the
test attestation chain. The database `host-view.db` lands in the working
directory.

## Line by line

### The imports

`booleanstatedef`, `levelcontroldef` and `onoffdef` are the typed packages
of the three clusters the host states something about
(`thingfabric.dev/go-fabric/cluster/spec/<cluster>`, imported as
`<cluster>def`). Each has a `Declare`, a `State`, a `Patch` and a typed
`Server`. `go doc thingfabric.dev/go-fabric/device` lists all of them.
`contract` is for the change's origin; `device` and `node` are the facade.

### `key`

A device's `StableKey` is any `fmt.Stringer`. The node keeps the endpoint
number it gave the device under that key, in its store, so the same key
gets the same endpoint after a restart and a controller keeps its
automations. A host uses its own identifier — here a string type.

### `newBridge`

`device.NewDimmableLight` mounts every server cluster the DimmableLight
device type mandates, each with its default server; the
[device-type reference](../reference/device-types.md#dimmablelight) lists
them. The two declarations after the options set what differs from the
defaults: the light starts off, at level 128. A declaration is the one
form for everything about a cluster — initial state, features, a veto on
a controller's write, command handlers, the host port the server drives,
or a server of the host's own — and declaring an optional cluster mounts
it.

`device.NewContactSensor` mounts BooleanState, whose `StateValue` the host
owns: `true` is closed. The declaration states the value at start. The
server selects ChangeEvent unless the declaration selects otherwise, as
matter.js's BooleanStateServer does, so every change of `StateValue` emits
StateChange.

`light.OnOff().OnChanged` sees every change of the light's OnOff, with the
new state and a `contract.Change` — the same for every kit server, a
quieter attribute such as LevelControl's `CurrentLevel` included, whose
subscribers are reported at most once a second. `Change.Origin` says who made it:
`contract.OriginHostSet` for the host's own `Set`,
`OriginControllerWrite` for a controller's attribute write,
`OriginCommand` for a command (On, Off, Toggle, …). The program forwards only what did not come from the
host, so its own `Set` does not echo back to the lamp.

Every callback of the facade follows one rule: the plain form
(`OnChanged`, `Observe`, `OnHealthSample`, `OnOTAProviderStatus`,
`OnDiagnosticEvent`, `ICD.OnModeChange`) runs inline on the event's
goroutine and must not block, the `…Async` form queues it on the
device's or node's own goroutine, one call after the other, and both
return the unsubscribe function.

`OnChanged` runs inline, after the change is stored and before the
command that made it answers. A callback that may block uses
`OnChangedAsync`, which runs on the device's own goroutine, one call
after the other. A lamp whose switching can fail is declared as the
cluster's host port instead (`onoffdef.Declare{Sink: relay}`): an error
from the sink answers the command with FAILURE and stores nothing.

### `doorMoved`

The host's own change: `b.door.BooleanState().Set` stores the new value
and reports it to every subscriber. `Set` is safe from any goroutine.

### `newNode`

`node.New` builds the node from its configuration: the stores, the root
and Aggregator endpoints, PASE and CASE, the subscription engine, mDNS.
`Aggregator().Add` puts each device under the Aggregator; before `Start`
it only changes the set, after `Start` it reassembles the running bridge.
A stored endpoint number is never dropped because its device is absent:
a device a previous run served keeps its number until it returns, and a
device the host removes keeps it too (`Remove`). `Forget` frees it, for a
device that is gone for good. A `Start` that fails leaves the node
startable, as matter.js's `Node.start` does, with the store `New` opened
still open; the program gives up, so it calls `Stop` on that path and on
every later one.

A host whose devices come from several sources that load on their own
gives each a partition: `Aggregator().Partition(name)` has the same
`Add`, `Replace`, `Remove` and `Forget`, under its own snapshot scope.
`RetirePartition(ctx, name)` drops the stored numbers of a source the
host no longer has, without a handle, reserved ones included.
`Batch(ctx, fn)` applies many changes and reassembles once after `fn`,
and what applied stays applied when another change fails. It is judged
against its end state: a change that collides with a key another
endpoint serves, or a key already in the set, returns nil at the call
and is held until `fn` returns; then it applies if the collision is gone
— a `Remove` or `Forget` of the other endpoint, before or after it in
`fn`, gives the same outcome — or is refused. Those refusals and the
errors of a held change arrive in `Batch`'s result, not from the call
(`ErrDuplicateKey`, `ErrUnknownKey` and the others its doc names); a
call after `fn` returned is `ErrBatchDone`. A `Replace` by a
declaration the specification level withholds takes the device off the
wire as `Remove` does — the key keeps its number — and records it as
withheld in `Node.Surface`; a replacement that fails validation changes
nothing. A `Replace` by another device than the one served, or a
`Remove` and an `Add` of its key in one batch, keeps the endpoint number
and announces the new device, as matter.js announces a re-added
endpoint: every cluster gets a new DataVersion and every readable
attribute but the changes-omitted ones is reported to running
subscriptions. What a controller wrote
under the key — a label, scenes, a binding, a running Identify — and the
ConfigurationVersion stay with the key; an invoke the old device is
handling completes on it. A `Replace` by the device already served
announces nothing. An `Add` or `Replace` of a device whose own key or a part key
another endpoint already serves is refused with `ErrDuplicateKey` and
changes nothing. The Aggregator's own
methods are the default partition's, so a host with one source writes
none of this.

### `config`

The node's identity. The passcode, discriminator, PBKDF parameters,
vendor and product are the CSA test values; a product sets its own and
its attestation chain (`Config.Attestation`). `Store.Path` makes the node
open the SQLite file itself and close it on `Stop`. The
[configuration section](../getting-started/device.md#configuration) lists
what `Config.Validate` checks and what each zero value means.

### `run`

`Start` assembles the endpoints and opens the commissioning window when no
fabric is stored. `n.Topology()` is what the node now serves: the
Aggregator's endpoint and each bridged device with its key, endpoint,
device type, name and reachability. The program logs each device's name
and endpoint. A device also carries its `Scope` — the name of the
Aggregator partition that holds it (`node.Scope`, `"node"`, for the
default one, so `n.Aggregator().Partition(d.Scope)` gives the partition
back; `Aggregator.Partitions()` lists them) — its `Tags`, the semantic
tags its Descriptor serves as a controller reads them (none while the
Descriptor serves no TagList — the default without `device.Options.Tags`,
and this example's case; the feature Apple's stack refuses), and its `Labels`, the FixedLabel entries
then the UserLabel entries (`User` set) it serves, as a controller reads
them. `Added` is found in every partition.

Then the device's lifecycle, each a single call on the device:
`Rename` changes the NodeLabel every controller sees and is persisted, so
it survives a restart; `SetReachable(ctx, true)` reports the door
reachable — `false` is what a host calls when it loses the device, and
sets the bridged device's Reachable attribute and its ReachableChanged
event. A device starts reachable. `EndpointNumber` (used by the test)
returns the endpoint the node gave the device.

`SetupPayload` returns the QR payload and the manual code for the
configured passcode and discriminator. The program then serves until the
signal context ends and stops the node.

### `main`

The signal context ends on Ctrl-C or SIGTERM; `run`'s error ends the
process with status 1.

## The test

`examples/host-view/main_test.go` runs the same `newBridge` on a
`fabrictest` site: a controller commissions the node, reads the door's
`StateValue` after `doorMoved(false)`, and invokes Toggle. The handler sees
exactly the controller's switch-on, not the host's own `Set` before it.
A second test starts `newNode` on loopback without mDNS and checks
`Topology` and `Rename`. The [testing guide](testing-in-process.md) explains
the site.

*Source: `examples/host-view/main.go`, `examples/host-view/main_test.go`, `device/doc_gen.go`, `device/lifecycle.go` (`SetReachable`, `Rename`, `EndpointNumber`), `node/lifecycle.go` (`Topology`), `node/aggregator.go` (`Add`, `Forget`), `cluster/spec/onoff/server_gen.go` (`Declare`, `OnChanged`, `OnChangedAsync`), `contract/change.go` (`Change`, `Origin`).*
