# reference-bridge

A runnable Matter bridge built **only from `go-fabric`'s public API** — no
`internal/`, no `_test.go` helper, no fork. It bridges a hard-coded fleet of
simulated devices — one per surface the module serves — advertises itself
over mDNS, accepts a commissioner over PASE and serves the operational CASE
session that follows. It is also the device the chip-tool suite
(`internal/chiptool`) and the borrowed CSA certification cases run against.

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
| 2 | OnOffLight `0x0100` | `demoLight`, a `contract.EndpointSource` with a hand-written OnOff (`0x0006`) cluster server |
| 3 | TemperatureSensor `0x0302` | `demoThermometer`, a `contract.FloatMeasurementSource` — no cluster code at all; the assembler picks the cluster from the declared measurement class |
| 4 | WaterValve `0x0042` | `demoValve`, the host port behind the library's `cluster/valve` server (ValveConfigurationAndControl `0x0081`) |
| 5 | ModeSelect `0x0027` | `demoSelector`, the host port behind `cluster/modeselect` (ModeSelect `0x0050`) |
| 6 | Speaker `0x0022` | `dimmer`, the host port behind `cluster/levelcontrol` (LevelControl `0x0008`), with the OnOff cluster its "with On/Off" commands drive |
| 7 | ColorTemperatureLight `0x010C` | the same `dimmer` with the Lighting surface (`fleet_lighting.go`): LT OnOff, the module's LevelControl extended by the LT attributes, `cluster/light` ColorControl |
| 8 | Fan `0x002B` | `demoFan`, host port of `cluster/fan` (MultiSpeed, Auto, Step) |
| 9 | SmokeCoAlarm `0x0076` | `demoSmokeAlarm`, host port of `cluster/alarm` with every optional attribute, plus the battery as PowerSource |
| 10 | Pump `0x0303` | `demoPump`: OnOff, `cluster/pump`, and a FlowMeasurement |
| 11 | FlowSensor `0x0306` | a `demoReading` (`fleet_sensors.go`) |
| 12 | LaundryWasher `0x0073` | `demoWasher`: `cluster/opstate` OperationalState and `cluster/modebase` LaundryWasherMode (`fleet_appliances.go`) |
| 13 | RoboticVacuumCleaner `0x0074` | `demoVacuum`: RvcOperationalState, RvcRunMode, RvcCleanMode |
| 14 | Thermostat `0x0301` | `cluster/thermo` |
| 15 | WindowCovering `0x0202` | `cluster/cover` |
| 16 | DoorLock `0x000A` | `demoLock`, host port of `cluster/lock` |
| 17-19 | Humidity, Occupancy, Contact sensors | `demoReading` / `demoBinary` measurement sources |
| 20 | GenericSwitch `0x000F` | `demoButton`, a momentary press source with long-press |
| 21 | AirPurifier `0x002D` | `demoAirPurifier`: the fan's FanControl plus `cluster/filter` HEPA and activated-carbon filter monitoring |
| 22 | Closure `0x0230` | `demoGarage`: `cluster/closure` ClosureControl (Positioning, Ventilation) with a simulated drive (`fleet_closure.go`) |

The root endpoint carries the node's own clusters, TimeSynchronization and
DiagnosticLogs among them (`wiring.go` buildRootClusters, which also says why
IcdManagement and the OTA requestor are left out).

The shapes are deliberately different. A device with commands has to serve
`contract.ClusterServer` itself; a read-only measurement does not, and the
difference in what the host has to write is large. The last three write no
cluster code either: the library carries the server, and the device
implements only that server's narrow host port — the shape a consumer with a
cluster the library already models is meant to reach for.

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
  `Apply` runs it. `persist.go` calls both and writes no SQL, so a schema
  change arrives with the dependency bump. A host with a migration tool
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
After a restart `main` loads the CASE identities, then calls
`Bridge.ReestablishFormerSubscriptions`: the bridge resolves each controller
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
- **Fabric teardown.** `RemoveFabric` persists and drops the fabric's
  persisted subscriptions, but no session, live subscription or resumption
  record is evicted, and the operational mDNS record is not withdrawn.
- **A closing commissioning window.** The configured passcode's window is
  open for the process lifetime. AdministratorCommissioning and the enhanced
  (multi-admin) window are wired (`commissioning.go`); the basic window is
  not.
- **Reassembly.** `Bridge.Reassemble` is never called; the fleet is fixed.
- **Persisted `GeneralDiagnostics` counters** (RebootCount,
  TotalOperationalHours).
