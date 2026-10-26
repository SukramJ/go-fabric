# Embed in a daemon

[A host's view](host-view.md) is go-fabric as a whole program written for
the page. This page is the same pattern inside a daemon that already
exists: [go-zendure2mqtt](https://github.com/SukramJ/go-zendure2mqtt), which
bridges Zendure energy stores to MQTT, gains an optional Matter side. Each
Zendure unit becomes one bridged endpoint, and a Matter controller shows
the unit's charge level and power. The daemon's pull request
[SukramJ/go-zendure2mqtt#64](https://github.com/SukramJ/go-zendure2mqtt/pull/64)
does it in one commit per step, and each step below links its commit.

The daemon imports `node`, `device`, `contract` and the `<cluster>def`
packages, and nothing below them. A test in the daemon holds that
([`TestFabricImportsOnlyTheFacade`](https://github.com/SukramJ/go-zendure2mqtt/blob/1d552bf10c895a270780bf5c946cab55dc9fd777/internal/fabric/fabric_test.go)).
It does the work of go-fabric's `script/archcheck` in one function.

## 1. The dependency

```sh
go get github.com/SukramJ/go-fabric@v0.10.8
```

A daemon pins a tag, never a commit. go-fabric is Apache-2.0. Its direct
dependencies are six permissive modules (see
[`THIRD-PARTY-NOTICES.md`](https://github.com/SukramJ/go-fabric/blob/main/THIRD-PARTY-NOTICES.md)),
and the pure-Go SQLite driver is the largest of them. A daemon with a
licence gate runs it over the new graph. go-licenses does not classify
`modernc.org/mathutil`'s BSD-3-Clause LICENSE file, so the daemon ignores
that one module by name, with the reason in its Makefile.

Commit: [`6de3e76`](https://github.com/SukramJ/go-zendure2mqtt/commit/6de3e768459e99250cf83693451665588872567d).

## 2. `node.Config` from the daemon's config

The daemon has its own config file, so the node's settings are a block in
it. The block is off by default, so nothing changes for an installation that
does not set it:

| Key | Default | `node.Config` |
| --- | --- | --- |
| `MATTER_ENABLED` | `false` | — (nothing is built) |
| `MATTER_LISTEN` | `:5540` | `Network.Listen` |
| `MATTER_STORE_PATH` | a file next to the config file | `Store.Path` |
| `MATTER_PASSCODE`, `MATTER_DISCRIMINATOR` | `20202021`, `3840` | `Commissioning.Passcode`, `.Discriminator` |
| `MATTER_VENDOR_ID`, `MATTER_PRODUCT_ID` | `0xFFF1`, `0x8001` (CSA test vendor) | `ProductDescription` |
| `MATTER_LABEL` | the daemon's name | `BasicInformation.NodeLabel` |

The facade generates no commissioning values: a zero passcode fails
`Config.Validate`. The defaults are therefore the public values the
reference bridge pairs with, and the daemon's README tells the operator to
change the passcode before pairing for real. The PBKDF parameters
(`Commissioning.PBKDFIterations`, `.Salt`) are constants of the daemon.
Validation in the daemon refuses an enabled block that the node would refuse
(a listen address without a port, a store path in a directory that does not
exist, a trivial passcode) in the same way it refuses its other keys.

Two fields are easy to forget:

- `Logger` is the daemon's own `*slog.Logger`. The node then logs through
  the daemon's handler and level, and a log-level change reaches it too
  (ADR 0026 principle 9).
- `BasicInformation.SoftwareVersion` and `.SoftwareVersionString` carry the
  daemon's version, not go-fabric's.

`node.New(cfg)` builds the node and binds nothing yet.

Commits: [`3cfa56c`](https://github.com/SukramJ/go-zendure2mqtt/commit/3cfa56c7d53387fdf83b11367964a76c378f1d29)
(the block), [`6de3e76`](https://github.com/SukramJ/go-zendure2mqtt/commit/6de3e768459e99250cf83693451665588872567d)
(`NodeConfig`).

## 3. The device: one daemon entity as a typed declaration

Pick the device type in two steps. A device that measures gets the sensor
device type of its measurement. A device with a battery declares
PowerSource, which BridgedNode allows on every bridged device under the
condition `BridgedPowerSourceInfo`. A Zendure unit measures AC power and
carries a battery, so it is an **ElectricalSensor** with a PowerSource. In
the typed form (the release after v0.10.8; the daemon's v0.10.8 form follows
below) the declaration is:

```go
dev, err := device.NewElectricalSensor(device.Options{
	StableKey: device.StringKey("zendure:" + sn), Name: name, Vendor: "Zendure", DeviceAddress: sn,
},
	powertopologydef.Declare{Features: powertopologydef.FeatureNodeTopology},
	electricalpowermeasurementdef.Declare{Features: electricalpowermeasurementdef.FeatureAlternatingCurrent,
		Initial: electricalpowermeasurementdef.Patch{PowerMode: …, NumberOfMeasurementTypes: …, Accuracy: …}},
	powersourcedef.Declare{Features: powersourcedef.FeatureBattery,
		Initial: powersourcedef.Patch{Order: new(uint8(0)), BatReplacementNeeded: new(false), …}},
)
```

- PowerTopology is mandatory, and the kit applies no feature selection to
  it. An empty declaration is refused because PowerTopology needs one of
  NodeTopology, TreeTopology and SetTopology. NodeTopology, which says the
  measurement applies to the endpoint itself, is the smallest choice that
  conforms.
- ElectricalPowerMeasurement needs AC or DC, and the host states its
  `Accuracy` (one entry for ActivePower) and its `NumberOfMeasurementTypes`.
  matter.js has no defaults for either.
- PowerSource's `EndpointList` names the device's own endpoint. The kit
  fills it, together with Status, Description and BatChargeLevel. The host
  states `Order` and `BatReplacementNeeded`.
- The declared PowerSource states `BridgedPowerSourceInfo` for the bridged
  node, under which BridgedNode makes PowerSourceConfiguration mandatory
  (its conformance `BridgedPowerSourceInfo, D` only announces a
  deprecation). The kit mounts it with its default server, so the device
  reports no device-type violation; `powersourceconfigurationdef.Declare`
  states its `Sources` when the host wants them listed.

The stable key is the unit's serial number, the identity the daemon already
uses for its MQTT topics. Under it the unit keeps its endpoint number
across restarts.

**v0.10.8 and the battery.** `powersourcedef.Declare` on an ElectricalSensor
needs the release after v0.10.8, which carries this cookbook's facade fix
(CHANGELOG, Unreleased). At v0.10.8 the kit refuses the declaration because
the type does not list the cluster. The daemon therefore takes the route
v0.10.8 has for a battery beside a measurement: `device.Measurement(src)`
with a source of class `contract.MeasurementBattery`. The kit serves that
source as the device's PowerSource, with BatPercentRemaining from the
percentage. At v0.10.8 that battery's changes reach a subscriber only
through `device.WithChangeNotifier(src)`; the second fix of this page's
pull request makes the kit's server forward them itself. The daemon moves
to the typed declaration when a release carries both fixes.

Commit: [`0bac952`](https://github.com/SukramJ/go-zendure2mqtt/commit/0bac9524e35d232fc077b339372b19ac83dfa8a1)
([`internal/fabric/projection.go`](https://github.com/SukramJ/go-zendure2mqtt/blob/1d552bf10c895a270780bf5c946cab55dc9fd777/internal/fabric/projection.go) `newUnit`,
[`internal/fabric/battery.go`](https://github.com/SukramJ/go-zendure2mqtt/blob/1d552bf10c895a270780bf5c946cab55dc9fd777/internal/fabric/battery.go)).

## 4. The aggregator batch at the device run

The batch is the one from `newNode` in [A host's view](host-view.md#newnode):
`Aggregator().Add` for each device. A daemon has to decide when a device
is added and when it is gone:

- **At start** the daemon adds every device its backend lists
  (`Source.Devices()`) before `Start`, so the first reading finds its
  endpoint in place.
- **A device that appears later** goes through the same `Add`, on a running
  node, which then reassembles. A cloud login can list a unit after the
  start, and a reading can name a unit the node does not serve yet.
- **A slow login** renumbers nothing: the stored endpoint number of a
  unit not yet listed is kept however long the list takes, since the node
  never drops a number because its device is absent (v0.13.0 removed
  `MarkComplete`, the call that let it).
- **A device that vanishes:** the daemon never removes a unit (its state
  and its MQTT side keep every serial it saw), and the projection mirrors
  that. A unit the backend no longer reaches becomes unreachable (step 6).
  `Aggregator().Remove` is for a host that does drop a device.

Commit: [`0bac952`](https://github.com/SukramJ/go-zendure2mqtt/commit/0bac9524e35d232fc077b339372b19ac83dfa8a1)
(`addKnown`, `ensure`, `markComplete`).

## 5. Readings to attributes

The daemon's readings already go through one fan-out, the coordinator,
which publishes them to MQTT. The node is attached there as one more
consumer, next to Home Assistant discovery, and does not get a second
reading path. Each reading becomes a `Set` on the typed server, with only
the attributes that changed:

```go
mw := int64(math.Round((gridInput - homeOutput) * 1000)) // positive imported
err := dev.ElectricalPowerMeasurement().Set(electricalpowermeasurementdef.Patch{ActivePower: &mw})
```

ActivePower is in milliwatts, and its sign follows the cluster: positive
for power imported, negative for power exported (matter.js
`electrical-power-measurement.resource.ts:159-162` at `862ad75f`). The
battery level is a percentage, and PowerSource serves it as
BatPercentRemaining in half-percent (80 % is 160). The kit stores each
`Set`, advances the cluster's data version and reports the change to
every subscribed controller, so the daemon does no reporting of its own.

Commit: [`3c05e46`](https://github.com/SukramJ/go-zendure2mqtt/commit/3c05e46ca718fbf46bf3bf13c9bb63ed5906bfe9)
([`internal/fabric/readings.go`](https://github.com/SukramJ/go-zendure2mqtt/blob/1d552bf10c895a270780bf5c946cab55dc9fd777/internal/fabric/readings.go)).

## 6. Lifecycle and reachability

- **Start before the backend runs, stop after it.** The node starts after
  the daemon's MQTT link and before the coordinator drives the backend. It
  stops on shutdown after the backend has stopped and before the MQTT
  link closes. With `MATTER_ENABLED` set, a failing `node.New` or `Start`
  is a startup error of the daemon. It never falls back silently to a
  daemon without its Matter side.
- **`SetReachable`** follows the backend's own word. Each
  `DeviceReachable(dev, ok)` of the daemon's observer becomes
  `dev.SetReachable(ctx, ok)`, which sets BridgedDeviceBasicInformation
  `Reachable` and emits its ReachableChanged event. `SetReachable` is
  idempotent, so the backend's repeated calls cost nothing.
- **Upstream down.** While the backend's upstream is unusable (the cloud
  session dropped, or no local unit answers) every unit is unreachable.
  When the upstream is back, each unit returns to its own reachability.

The coordinator remains the backend's only observer and hands every
reading and observer call to an optional `Projection`. With the block off
that projection is nil, and the MQTT side runs exactly as before.

Commit: [`c687e7e`](https://github.com/SukramJ/go-zendure2mqtt/commit/c687e7e9c6f903e85eb5a221d3910402dc922273).

## 7. The pairing code into the UI and MQTT

`Node.PairingCodes()` returns the QR payload and the manual code for the
configured passcode and discriminator, before and after `Start`. The daemon
puts them in three places:

- one info log line at start (`zendure2mqtt.matter_pairing`);
- the retained `<name>/info`, where the daemon already publishes its build
  and connection;
- the diagnostic web UI's status line, served from `/api/health`.

A Home Assistant entity for the code was left out: the code pairs only
while the commissioning window is open, and the window is open only until
the first controller has paired.

Commit: [`f603c28`](https://github.com/SukramJ/go-zendure2mqtt/commit/f603c281a09d94205c2eb5ba2c92e1241c7db513).

## 8. Running it

The daemon's packaging decides how it runs: its Dockerfile, its Home
Assistant add-on, a service unit. go-fabric ships none of these. What the
node needs from that packaging:

- the store file on persistent storage, since losing it unpairs the bridge;
- UDP 5540 and mDNS on the controllers' LAN, which in a container means
  host networking;
- IPv6 on that LAN.

The [operations page](operations.md) lists all of it from the node's side:
files, ports, logs, health, limits, reset and upgrade. The daemon's README
section "Matter controllers" is the operator's version.

Commit: [`1d552bf`](https://github.com/SukramJ/go-zendure2mqtt/commit/1d552bf10c895a270780bf5c946cab55dc9fd777).

## What you did not have to do

The import test forbids every package below the facade, and the daemon
needed none of them:

- no cluster ids, attribute ids or TLV: the typed `Declare`, `Patch`
  and `Set` of each `<cluster>def` package;
- no `bridge`, `endpoint` or `im`: the Aggregator assembles the endpoints,
  their Descriptor and BridgedDeviceBasicInformation, and the subscription
  engine reports what `Set` stored;
- no `mdns`: the node advertises itself, with the discriminator subtype
  that Apple Home and Google Home browse for;
- no `secure`, `transport` or session code: PASE, CASE, MRP and the
  commissioning window are the node's own;
- no `store`: `Store.Path` is one SQLite file that the node opens and closes.

## What a controller sees

The daemon's tests commission the node with go-fabric's in-process
controller ([`fabrictest`](testing-in-process.md)). They read what Apple
Home or Home Assistant would read from the same node:

- one endpoint per unit under the Aggregator, whose Descriptor lists
  ElectricalSensor and BridgedNode and serves PowerTopology,
  ElectricalPowerMeasurement and PowerSource;
- `NodeLabel` as Home Assistant names the unit, and `Reachable`;
- after a reading, `BatPercentRemaining` 160 for 80 % and `ActivePower`
  −600000 for 600 W fed to the home;
- a second reading arriving through a subscription, as it would to a
  controller;
- `Reachable` false after `DeviceReachable(false)`, again through the
  subscription.

A controller shows this as a battery level and a power reading on one
accessory. The bridge carries the CSA test vendor and go-fabric's test
attestation chain, so a controller may warn that it is uncertified. The
proof that this works is the daemon pull request's tests together with a
real pairing against a unit on the LAN. That pairing is the owner's manual
step, and this page does not claim it.

*Source: the daemon pull request
[SukramJ/go-zendure2mqtt#64](https://github.com/SukramJ/go-zendure2mqtt/pull/64);
`node/config.go` (`Config`, `Commissioning`, `Store`), `node/aggregator.go`
(`Add`), `device/electrical_sensor_gen.go`,
`device/base.go` (`withBridgedPowerSource`, `Device.PowerSource`),
`device/lifecycle.go` (`SetReachable`), `node/node.go` (`PairingCodes`).*
