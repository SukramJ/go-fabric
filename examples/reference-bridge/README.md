# reference-bridge

A runnable Matter bridge built **only from `go-fabric`'s public API** — no
`internal/`, no `_test.go` helper, no fork. It bridges a hard-coded fleet of
simulated devices — one per surface the module serves — advertises itself
over mDNS, accepts a commissioner over PASE and serves the operational CASE
session that follows. It is also the device the chip-tool suite
(`internal/chiptool`) and the borrowed CSA certification cases run against.

It is built the way a host is meant to build one (ADR 0018):

```go
n, err := node.New(cfg)                         // the node: root, Aggregator, sessions, PASE/CASE, window
lamp, err := device.NewOnOffLight(opts, ...)    // one device.New<Type> per device
err = n.Aggregator().Add(ctx, lamp)             // … for each device of the fleet
n.Aggregator().MarkComplete()                   // the fleet is complete from the first assembly
err = n.Run(ctx)                                // Start, wait for ctx, Stop
```

`main.go` holds the `node.Config` (the CSA test identity, the flags, the
root's labels and locales, the Aggregator's Actions server) and the steps
the facade leaves to the host on its accessors: the test event triggers
(`Root().GeneralDiagnostics`), the OTA applier (`ota.go`, handed over as
`node.OtaConfig.Applier`), the mDNS subtype responder, the providers that
name endpoints by StableKey (`fleet_node.go`), the banner and the topology
listing. `fleet.go` `endpoints` builds the devices:

- a device type's **default servers** where the module holds the state —
  the lights, the speaker and the pump take `cluster/onoff.Server` as
  their OnOff cluster and follow it through `Device.OnChanged`, and switch
  it with `onoff.Server.Set` when the device switches itself;
- **the host's own server** through `device.WithServer` where the cluster's
  rules need a host port (`cluster/valve`, `cluster/levelcontrol`,
  `cluster/fan`, `cluster/lock`, `cluster/thermo`, `cluster/opstate`, …),
  with `device.WithChangeNotifier` when the host model notifies its own
  changes;
- **parts** through `AddPart` (the fridge's and the oven's cabinets, the
  charger's PowerSource, DeviceEnergyManagement and ElectricalSensor, the
  grid meter's ElectricalSensor, the smoke alarm's battery);
- **measurement-backed sensors** through `device.WithMeasurement` (the
  thermometer, flow, humidity, occupancy, soil sensors, the button, the
  battery and the electrical sensors): the assembler materialises their
  clusters from the host's live reading by its measurement class.

It exists as the module's *second* consumer. The first is the daemon
go-fabric was extracted from, which remembers how the code used to be
arranged. This one does not, so what it costs to write is a measurement of
the seam rather than a demonstration of it.

```sh
go run ./examples/reference-bridge
go run ./examples/reference-bridge --help
```

Startup prints the pairing information on stdout:

```
  go-fabric reference bridge
  --------------------------------------------------------------
  listening on     [::]:5540
  bridged devices  21
  vendor/product   0xFFF1 / 0x8001  (CSA TEST identity — not shippable)

  discriminator    3840
  setup passcode   20202021
  manual code      34970112332
  QR payload       MT:-24J0AFN00KA0648G00

  pair with, e.g.:
    chip-tool pairing onnetwork-long 1 20202021 3840
  --------------------------------------------------------------
```

## The fleet

| Endpoint | Device type | Backed by |
| --- | --- | --- |
| 2 | OnOffLight `0x0100` | `demoLight`: `device.NewOnOffLight` with the default OnOff (`cluster/onoff.Server`, Lighting) and a FixedLabel |
| 3 | TemperatureSensor `0x0302` | `demoThermometer`, a `contract.FloatMeasurementSource` — no cluster code at all; the assembler picks the cluster from the declared measurement class |
| 4 | WaterValve `0x0042` | `demoValve`, the host port behind the library's `cluster/valve` server (ValveConfigurationAndControl `0x0081`) |
| 5 | ModeSelect `0x0027` | `demoSelector`, the host port behind `cluster/modeselect` (ModeSelect `0x0050`) |
| 6 | Speaker `0x0022` | `dimmer`, the host port behind `cluster/levelcontrol` (LevelControl `0x0008`), with the default OnOff its "with On/Off" commands drive |
| 7 | ColorTemperatureLight `0x010C` | the same `dimmer` with the Lighting surface (`fleet_lighting.go`): the default LT OnOff, the module's LevelControl extended by the LT attributes, `cluster/light` ColorControl |
| 8 | Fan `0x002B` | `demoFan`, host port of `cluster/fan` (MultiSpeed, Auto, Step) |
| 9, 10 | SmokeCoAlarm `0x0076` | `demoSmokeAlarm`, host port of `cluster/alarm` with every optional attribute; its battery is the PowerSource part (10) |
| 11 | Pump `0x0303` | `demoPump`: the default OnOff, `cluster/pump`, and a FlowMeasurement |
| 12 | FlowSensor `0x0306` | a `demoReading` (`fleet_sensors.go`) |
| 13 | LaundryWasher `0x0073` | `demoWasher`: `cluster/opstate` OperationalState and `cluster/modebase` LaundryWasherMode (`fleet_appliances.go`) |
| 14 | RoboticVacuumCleaner `0x0074` | `demoVacuum`: RvcOperationalState, RvcRunMode, RvcCleanMode, and `cluster/servicearea` ServiceArea with one map and two areas (`fleet_servicearea.go`) |
| 15 | Thermostat `0x0301` | `cluster/thermo` Thermostat and ThermostatUserInterfaceConfiguration |
| 16 | WindowCovering `0x0202` | `cluster/cover` |
| 17 | DoorLock `0x000A` | `demoLock`, host port of `cluster/lock` |
| 18-20 | Humidity, Occupancy, Contact sensors | `demoReading` / `demoBinary` measurement sources; the contact sensor adds BooleanStateConfiguration |
| 21 | GenericSwitch `0x000F` | `demoButton`, a momentary press source with long-press |
| 22 | AirPurifier `0x002D` | `demoAirPurifier`: the fan's FanControl plus `cluster/filter` HEPA and activated-carbon filter monitoring |
| 23 | Closure `0x0230` | `demoGarage`: `cluster/closure` ClosureControl (Positioning, Ventilation) with a simulated drive (`fleet_closure.go`) |
| 39 | ClosurePanel `0x0231` | `demoPanel`: `cluster/closure` ClosureDimension as connectedhomeip's closure-app panel serves it, with its simulated motion (`fleet_closure.go`) |

The root endpoint carries the node's own clusters, TimeSynchronization,
DiagnosticLogs and the OTA requestor among them — the node builds them
(`node/root.go` buildRoot, which also says why IcdManagement is left out).

The shapes are deliberately different. A read-only measurement writes no
cluster code at all. A device type's default servers hold their state
themselves and need nothing but the host's hooks. A cluster whose rules
reach the device writes no cluster code either: the library carries the
server, and the device implements only that server's narrow host port —
the shape a consumer with a cluster the library already models is meant
to reach for.

Endpoint numbers are the assembler's to assign and are shown here as they
come out of an empty store; nothing reads them back as constants.

## Test controls (off by default)

Two flags let a test make the devices do what a controller cannot ask for —
the same two mechanisms CHIP's own example apps take (`control.go`):

- `--app-pipe <path>` creates a named pipe that takes one JSON command per
  line, in CHIP's shape: `{"Name": "SimulateLongPress", ...}`,
  `SetBooleanState`, `SetOccupancy`, `OperationalStateChange`,
  `ErrorEvent`, `Docked`, `Reset`, `ChargerFound`, `Charging`, `Charged`,
  plus this fleet's own (`SetSensorValue`,
  `SetLocalTemperature`, `SetLockJammed`, `PumpEvent`, …). The CHIP Python
  certification cases drive a device through exactly this pipe.
- `--enable-key <hex>` arms GeneralDiagnostics TestEventTrigger with that
  test enable key; the SmokeCoAlarm and ClosureControl triggers are CHIP's.
- `--mdns-os-hostname` advertises the OS host name as the SRV target instead
  of the MAC-derived one, for a test host whose LAN interface has no IPv6:
  the OS responder then publishes the address records — the IPv6 link-local
  ones of every interface an IPv6-only controller needs included.
- `--ota-harness` applies a downloaded OTA image in place: the root's
  BasicInformation reports the image's version at once and the OTA
  requestor reports the update as after a restart (VersionApplied,
  NotifyUpdateApplied), which is what the CHIP SU cases read. It also lets
  the requestor consent to an update and query 250 ms after an
  announcement, as matter.js's cert requestor does. Without it the daemon
  validates a downloaded image and keeps it under `<ota-dir>/applied/`,
  still running the old version; `--ota-dir` (default `ota` beside `--db`)
  is where downloads land.

- `--wifi` (TESTING ONLY) serves WiFiNetworkDiagnostics on the root from a
  simulated Wi-Fi link (`wifi.go`): fixed values (a locally administered
  BSSID, WPA2, 802.11ax, channel 36, RSSI -52 dBm), the PKTCNT and ERRCNT
  counters and CurrentMaxRate, and the RootNode condition `WiFi` stated.
  The app pipe's `{"Name": "WiFiDiagnostics", "Action": …}` steps the
  counters (`StepCounters`) and raises the three events
  (`Disconnection`, `AssociationFailure`, `ConnectionStatus`); with
  `--enable-key`, CHIP's Wi-Fi TestEventTrigger codes
  (`0x0036000000000000` to `…02`) raise them as CHIP's all-clusters app
  does. NetworkCommissioning stays Ethernet-only. A real Wi-Fi host reads
  the values from the kernel (nl80211, or `iw`) behind its own
  `mattercore.WiFiReporter` and hands it to `node.Network.WiFi`.

- `--ota-provider` (TESTING ONLY) serves one embedded test OTA image
  (`ota_provider.go`) through the node's OTA Software Update Provider
  (ADR 0021): an OtaProvider endpoint beside the Aggregator, the image —
  software version 2 of the test vendor/product, 4 KiB of pattern bytes in
  the OTA image format — over BDX to a requestor that asks with version 1.
  The user's consent is granted and an allowed apply proceeds at once; the
  app pipe's `{"Name": "OtaProviderConsent", "Action": "withhold"}` makes
  the image need a consent the provider denies, `"grant"` restores it. A
  product's provider serves the images its vendor signed behind its own
  `mattercore.OTAImageSource`.

- `--dcl prod|testnet|off` (default `off`) looks the image `--ota-provider`
  offers up in the CSA Distributed Compliance Ledger (`dcl.go`,
  `controller/dcl`): is the version listed for the requestor's vendor and
  product, and does the image match the ledger's checksum. The answer is
  logged; the image is offered either way. What leaves the host when it is
  on: one HTTPS request per offer, naming the requestor's vendor id,
  product id and the offered software version. Off, nothing contacts the
  ledger.

Each applied command is logged as `apppipe.applied`. None of these flags
belongs in a real deployment.

## This is a TEST identity

The bridge advertises vendor `0xFFF1`, product `0x8001`, and presents the
attestation chain from `attestation.BuildTestChain` — the CSA **test** PAA
from the Matter specification's test vectors.

That is what makes this example pairable at all: chip-tool, Apple Home and
Google Home all ship the matching test PAA in their trust stores, so no
vendor-supplied DAC is needed and no `--bypass-attestation-verifier` — the
example also serves the matching test Certification Declaration
(`attestation.BuildTestCertificationDeclaration`); without one, a
commissioner that verifies attestation stops the pairing.

It is also why nothing built on it may ship. `0xFFF1` is reserved for test
and development; a device advertising it is not a product identity, cannot be
certified, and must not be sold or described as Matter-compliant. A real
product replaces the vendor/product pair *and* all four attestation inputs
(`DAC`, `DACPrivateKey`, `PAI`, `CertificationDeclaration`) with material
issued under its own CSA membership. See the repository README's
"Not certified" section.

## Persistence: what it costs, and what breaks without it

Endpoint numbers are the one piece of bridge state a controller caches and
cannot be told to re-read: it keys its accessory list on the number. An
in-memory store makes the example *run* and quietly makes it *wrong* across
a restart — every commissioned controller loses every accessory, or worse,
silently rebinds an accessory to a different device that inherited its
number.

So this example persists, and after the first version of it was written by
hand, both halves it had to hand-write moved into the module:

- **`endpoint/sqlitestore` is the production `endpoint.Store`.**
  `endpointtest.NewFakeStore` remains in-memory test scaffolding;
  `sqlitestore.New(db)` is what a bridge runs on. Neither package imports a
  driver — they take an already-open `*sql.DB` — so the host picks the
  driver and the DSN. This one uses `modernc.org/sqlite`, already a direct
  requirement of `go-fabric`, so persisting adds no new dependency.
- **The module owns the DDL.** `store.Schema()` and `sqlitestore.Schema()`
  return the embedded text each package's own queries are written against;
  `Apply` runs it. `persist.go` calls both and writes no SQL, and the node
  applies them again on the handle it is given, so a schema change arrives
  with the dependency bump. A host with a migration tool
  feeds the two scripts through that instead.
- **Numbers only ever advance.** The high-water mark lives in
  `matter_endpoint_allocation`, and `RemoveEndpoint` never returns a number
  to the pool.
- **Keys are plain strings here.** The fleet's `StableKey`s are
  `endpoint.StringKey`, so the store's default decoding is correct. A host
  with a composite key type must pass `sqlitestore.WithKeyDecoder`: without
  it the assembler's garbage collection cannot match a listed key against a
  live one and deletes every endpoint number on the first model-complete
  assembly.

Subscriptions persist too (`docs/adr/0008`, as matter.js does): each CASE
subscription is recorded in `matter_server_subscriptions` while it is active.
After a restart the node (`node.Start`) loads the CASE identities, then
calls `Bridge.ReestablishFormerSubscriptions`: the bridge resolves each controller
over mDNS, opens CASE to it as the initiator and re-sends the priming report
under the old subscription id, so the controller carries on without
re-subscribing. A controller it cannot reach within two seconds recovers on
its own. CASE sessions themselves stay volatile — Matter treats them so.

Delete `reference-bridge.db` to factory-reset.

## What this example does NOT do

Each of these is real production wiring that the loom daemon carries and this
one omits, listed so the omission is not mistaken for "not needed":

- **IPK rotation.** The CASE identity is derived from `IdentityRecord.IPK`
  only. A `KeySetWrite` that rotates the IPK carries up to three epoch keys in
  `GroupKeySetID=0`, and a correct bridge tries every one when matching an
  inbound `Sigma1.DestinationID`.
- **CASE resumption as responder.** `sigma.Responder.SetResumptionStore` is
  not wired, so every controller reconnect runs a full Sigma1–3 handshake
  instead of the one-round-trip resume. (The initiator that re-establishes
  subscriptions does offer a stored record and stores the new one.)
- **Reassembly.** The fleet is fixed: every device is added before the
  node starts, and none is added or removed while it runs (a running
  node's `Aggregator().Add` / `Remove` reassemble).
