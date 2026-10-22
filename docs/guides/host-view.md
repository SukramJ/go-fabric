# A host's view

This page is a whole bridge as the host that embeds go-fabric writes it:
a dimmable light and a door contact, declared once, served by a node, with
the changes a controller makes told apart from the host's own. The program
imports `node`, `device`, `contract` and three typed cluster packages —
nothing below the facade. It is `examples/host-view/main.go`; its test
commissions both devices on an in-process controller, reads the door and
toggles the light.

## The program

--8<-- "examples/host-view/main.go"

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
(`github.com/SukramJ/go-fabric/cluster/spec/<cluster>`, imported as
`<cluster>def`). Each has a `Declare`, a `State`, a `Patch` and a typed
`Server`. `go doc github.com/SukramJ/go-fabric/device` lists all of them.
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
owns: `true` is closed. The declaration states the value at start.

`light.OnOff().OnChanged` sees every change of the light's OnOff, with the
new state and a `contract.Change` — the same for every kit server, a
quieter attribute such as LevelControl's `CurrentLevel` included, whose
subscribers are reported at most once a second. `Change.Origin` says who made it:
`contract.OriginHostSet` for the host's own `Set`,
`OriginControllerWrite` for a controller's attribute write,
`OriginCommand` for a command (On, Off, Toggle, …). The program forwards only what did not come from the
host, so its own `Set` does not echo back to the lamp.

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
`MarkComplete` says every device the host knows is added, so the node may
drop the stored endpoint numbers of devices that are gone. On an error
path the node is stopped: `New` already started its engine.

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
and endpoint.

Then the device's lifecycle, each a single call on the device:
`Rename` changes the NodeLabel every controller sees and is persisted, so
it survives a restart; `SetReachable(ctx, true)` reports the door
reachable — `false` is what a host calls when it loses the device, and
sets the bridged device's Reachable attribute and its ReachableChanged
event. A device starts reachable. `EndpointNumber` (used by the test)
returns the endpoint the node gave the device.

`PairingCodes` returns the QR payload and the manual code for the
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

*Source: `examples/host-view/main.go`, `examples/host-view/main_test.go`, `device/doc_gen.go`, `device/lifecycle.go` (`SetReachable`, `Rename`, `EndpointNumber`), `node/lifecycle.go` (`Topology`), `node/aggregator.go` (`Add`, `MarkComplete`), `cluster/spec/onoff/server_gen.go` (`Declare`, `OnChanged`, `OnChangedAsync`), `contract/change.go` (`Change`, `Origin`).*
