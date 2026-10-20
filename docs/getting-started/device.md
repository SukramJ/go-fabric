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

*Source: `node/doc.go:4-43`, `node/config.go:94-103`, `bridge/bridge.go:29-47`, `node/node.go:601-603`.*

## The program

--8<-- "examples/quickstart-device/main.go"

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

*Source: `examples/quickstart-device/main.go`, `examples/quickstart-device/main_test.go`, `examples/reference-bridge/main.go:178-201`, `node/config.go:144-145`.*

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

*Source: `node/config.go:28-139`, `node/config.go:215-279`, `node/config.go:333-381`, `node/node.go:109-120`.*

## Devices

The `device/` package has one constructor per Matter device type in scope:
`device.NewOnOffLight`, `device.NewDimmableLight`, `device.NewThermostat`
and so on. They are generated from the matter.js model. A constructor
mounts every server cluster the device type mandates, with a default server:
the module's own server where one runs on its own state — OnOff,
LevelControl, ColorControl, Binding, EcosystemInformation, and Switch,
DoorLock, WindowCovering and Thermostat for a feature selection their
server serves — otherwise the generated `spec.Server`, whose commands are
unimplemented until you handle them. A `NewDimmableLight` therefore dims:
a controller's MoveToLevel moves its level, and the "with On/Off" commands
switch its OnOff.
The endpoint assembler mounts Descriptor, Identify,
BridgedDeviceBasicInformation, Groups and ScenesManagement; the device
layer never does.

`device.Options` identifies the device:

- `StableKey` is required and unique. The assembler keeps the endpoint
  number under it, so the same key gets the same endpoint number after a
  restart. The quickstart's test checks this.
- `Name` becomes the bridged device's NodeLabel. `Vendor` is its
  manufacturer.
- `Reachable` is a probe for the device's reachability; nil means always
  reachable.

Options passed after `Options` change what the device serves:

| Option | Effect |
| --- | --- |
| `WithServer(srv)` | your own server for a cluster, replacing the default |
| `WithOptional(id)` | mounts an optional cluster (Groups and ScenesManagement are then the assembler's) |
| `WithFeatures(id, bits)` | the features of a cluster's default server |
| `WithElements(id, opts)` | optional attributes, commands and events of a default server |
| `WithInitial(id, values)` | initial attribute values of a default server |
| `WithMeasurement(src)` | a sensor's measurement clusters from your live reading |
| `WithChangeNotifier(n)` | tells the bridge your model changed state its servers read |
| `WithClientClusters(ids...)` | client clusters beyond the mandatory ones |

### Two directions of state

A device's state moves both ways, and the two directions use different
calls:

- **Controller to device.** `Device.OnChanged(clusterID, fn)` calls `fn`
  with the attribute ids that changed, and the typed
  `light.OnOff().OnChanged(fn)` with the new state. The quickstart uses it
  to log the light's new state. Both fire for every change of the
  attribute — a controller's command or write and your own `Set` alike;
  they do not tell the two apart. The controller-only hooks are
  `light.OnOff().HandleOn(fn)` (your handler replaces the server's own
  handling of the command) and `light.OnOff().OnWrite(fn)` (a veto on a
  controller's write). To refuse a command, for example when the physical
  lamp did not switch, give the light your own `onoff.Server` with a
  `Sink` (`onoff.Config.Sink` through `WithServer`). An error from the
  sink answers FAILURE and stores nothing.
- **Device to controller.** When the lamp is switched at the wall, call
  `light.OnOff().Set(onoffdef.Patch{OnOff: new(on)})` (`onoffdef` is
  `github.com/SukramJ/go-fabric/cluster/spec/onoff`). The value is stored
  and reported to subscribers, and the sink is not called. `Set` is safe
  from any goroutine, including from inside the sink.

*Source: `device/doc.go:4-27`, `device/defaults.go`, `device/options.go:18-43`, `device/options.go:91-215`, `device/device.go`, `cluster/onoff/server.go` (`Set`, `SetAttributes`, `Handle`).*

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

*Source: `node/aggregator.go:40-132`, `examples/reference-bridge/main.go:328-332`.*

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

*Source: `node/node.go:691-712`, `node/node.go:526-534`, `node/commissioning.go:426-454`, `node/node.go:641-662`.*

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

*Source: `node/node.go:34-36`, `node/node.go:538-599`, `node/node.go:95-108`, `bridge/bridge.go:1541-1556`.*

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
- **A fleet of simulated devices**, one per surface the module serves. The
  blind's position is persisted in the settings table.
- **Testing-only flags** for certification runs: the app pipe, the
  TestEventTrigger key, the OTA provider and its ledger check, the
  simulated Wi-Fi link and the media devices. None of these belongs in a
  product.

*Source: `examples/reference-bridge/main.go` (`parseFlags`, `checkFlags`, `run`, `nodeConfig`), `examples/reference-bridge/attestation.go`, `examples/reference-bridge/persist.go`.*
