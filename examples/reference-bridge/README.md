# reference-bridge

A runnable Matter bridge built **only from `go-fabric`'s public API** — no
`internal/`, no `_test.go` helper, no fork. It bridges a hard-coded fleet of
simulated devices — one per surface the module serves — advertises itself
over mDNS, accepts a commissioner over PASE and serves the operational CASE
session that follows. It is also the device the chip-tool suite
(`internal/chiptool`) and the borrowed CSA certification cases run against.

It is built the way a host is meant to build one (ADR 0018):

```go
n, err := node.New(ctx, cfg)                       // the node: root, Aggregator, sessions, PASE/CASE, window
lamp, err := device.NewOnOffLight(opts, ...)    // one device.New<Type> per device
err = n.Aggregator().Add(ctx, lamp)             // … for each device of the fleet
err = n.Run(ctx)                                // Start, wait for ctx, Stop
```

`main.go` holds the `node.Config` (the CSA test identity, the flags, the
root's labels and locales, the Aggregator's Actions server) and the steps
the facade leaves to the host on its accessors: the test event triggers
(`Root().GeneralDiagnostics`), the OTA applier (`ota.go`, handed over as
`node.OTAConfig.Applier`), the providers that name endpoints by StableKey
(`fleet_node.go`), the banner and the topology listing. The node builds
its own DNS-SD advertiser with the subtype responder (a nil
`node.Config.Advertiser`). `fleet.go` `endpoints` builds the devices:

- a device type's **default servers** where the module holds the state,
  declared through each cluster's typed `Declare` (`onoffdef.Declare{Initial}`,
  `thermostatdef.Declare{Features}`, …): the device follows a
  controller's change through `Device.OnChanged` and switches itself
  through the typed server's `Set` (`onoffdef.Patch{OnOff: …}`);
- **host ports** where the cluster's rules need the host's state, as the
  `Declare`'s port field (`valvedef.Declare{StateSource}`,
  `levelcontroldef.Declare{LevelSource}`, the mode clusters' `ModeChanger`,
  `tbrmdef.Declare{BorderRouter}`, …), with `device.WithChangeNotifier`
  when the host model notifies its own changes; a host's whole server is
  the `Declare`'s `HostServer` (the charger's `cluster/energy.NewEnergyEvse`
  with its `EvseHost`, `fleet_evse.go`; the EcosystemInformation server,
  `fleet_sensors.go`);
- **parts** through `AddPart` (the fridge's and the oven's cabinets, the
  charger's PowerSource, DeviceEnergyManagement and ElectricalSensor, the
  grid meter's ElectricalSensor, the smoke alarm's battery);
- **sensors** with their readings as the `Initial` of their measurement
  clusters' `Declare` (`tmdef`, `rhdef`, `fmdef`, the air-quality
  concentrations, `fleet_sensors.go`), set again through the typed `Set`
  when the simulated reading moves.

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
  certification cases drive a device through exactly this pipe. The pipe
  is owner- and group-writable only (0660); a pipe already at the path is
  reused only when it is the daemon user's own (its mode is then set to
  0660), and a symbolic link or another user's pipe there is refused.
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

- `--wifi-sim` (TESTING ONLY) gives the root's NetworkCommissioning the
  Wi-Fi feature instead of Ethernet, over a simulated driver
  (`wifisim.go`) that knows the `--wifi-sim-ssid` networks
  (comma-separated `ssid:passphrase`, an entry without a passphrase takes
  any; default `zigbeehome:password123`, the CHIP harness's network). A
  scan without an SSID finds them all, a scan for another SSID is
  NetworkNotFound; ConnectNetwork joins a known network after 200 ms with
  its passphrase, answers AuthFailure with another and NetworkNotFound for
  an unknown SSID; the daemon stays on its Ethernet address. It lets the
  commissioner's network steps run without a radio: `reference-controller
  pair --passcode 20202021 --discriminator 3840 --wifi-ssid zigbeehome
  --wifi-credentials password123`. The list rules and statuses are
  connectedhomeip's (credentials of 0, 5, 8–63 bytes or 64 hex digits; one
  network). The committed network is kept in the database and rejoined
  when the daemon starts, as chip's Linux driver does. Without the flag
  the composition is unchanged.

- `--tc-required` (TESTING ONLY) serves GeneralCommissioning's
  TermsAndConditions feature with version 1 and acknowledgement bit 1
  required — the values the CSA's CGEN cases take as
  `PIXIT.CGEN.TCRevision` and `PIXIT.CGEN.RequiredTCAcknowledgements` —
  and enables the extension `BD-chip-GeneralCommissioning-TermsAndConditions`.
  CommissioningComplete then fails with TCAcknowledgementsNotReceived until
  the commissioner has sent SetTCAcknowledgements: `chip-tool pairing …
  --tc-acknowledgements 1 --tc-acknowledgements-version 1`. A commissioner
  without terms-and-conditions support cannot pair the daemon. Without the
  flag the composition is unchanged.

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

- `--media` (TESTING ONLY) bridges two more devices after the fleet
  (`media.go`, ADR 0022): a CastingVideoPlayer stating ContentAppPlatform
  and PhysicalInputs (OnOff, MediaPlayback, KeypadInput, ContentLauncher,
  ApplicationLauncher with the platform feature, MediaInput, AudioOutput,
  Channel, TargetNavigator, LowPower, WakeOnLan; Messages with `--media-messages`) and a ContentApp
  (ApplicationBasic, ApplicationLauncher, AccountLogin, KeypadInput),
  each a simulation of connectedhomeip's tv-app — its transport, three of
  its channels, two inputs, three outputs, two targets and the test suite's
  content app (catalog vendor 123, `exampleid`, setup PIN `34567890`),
  whose ApplicationBasic.Status the player's LaunchApp, HideApp and StopApp
  move. The app pipe's `{"Name": "MediaPlaybackState", "Operation":
  "Playing"|"Paused"|"NotPlaying"|"Buffering"}` moves the transport.
  Nothing plays: a product's host implements the `cluster/media` ports
  over its own player. The player's Messages (`messages.go`) presents a
  message at its StartTime and completes it when its Duration is over, as
  the tv-app's MessagesManager does; nothing is shown.
- `--media-messages` (TESTING ONLY, with `--media`) adds Messages to the
  player, as chip's tv-app serves it on its endpoint 1. Off by default:
  connectedhomeip's TC-IDM-10.5 at the harness pin evaluates the Casting
  Video Player's `[Rev >= v2]` term for Messages against a placeholder
  revision 1 and reports the cluster disallowed (see
  `docs/certifiability.md`); the MESS family runs against this variant.
- `--tls-sim` (TESTING ONLY) serves TlsCertificateManagement and
  TlsClientManagement on the root (`node.Config.TLS`, `tls.go`) over a
  software P-256 key store: a client certificate's key is made at its
  ClientCSR and kept in `--db`, the CSR is a DER PKCS#10 request signed
  ecdsa-with-SHA256 and the nonce signature raw r‖s, as chip's
  certificate table makes them. For the TLSCERT and TLSCLIENT families,
  and for `--camera-sim`'s push transports.
- `--camera-sim` (TESTING ONLY) bridges a Camera and a Chime after
  connectedhomeip's camera-app (`camera.go`): CameraAvStreamManagement
  with every feature the camera-app's HAL reports and its available video,
  audio and snapshot streams, WebRtcTransportProvider,
  PushAvStreamTransport, CameraAvSettingsUserLevelManagement (DPTZ, MPAN,
  MTILT, MZOOM, MPRESETS), ZoneManagement (TWODCART, PERZONESENS,
  USERDEFINED, FOCUSZONES) and the camera-app's three chime sounds. The
  stream, PTZ and chime state persists in `--db`. Nothing is captured or
  sent. The camera-app's own switches the cases pass are mirrored:
  `--camera-deferred-offer` (SolicitOffer answers DeferredOffer true),
  `--camera-test-videosrc` (CaptureSnapshot returns a generated JPEG;
  without it the camera has no video device and the capture fails) and
  `--camera-test-audiosrc` (accepted, nothing observable). The app pipe
  takes the camera-app's `{"Name": "SetHardPrivacyModeOn", "Value": bool}`
  and `{"Name": "ZoneTriggered", "ZoneId": id | [ids]}` (the zone
  manager's trigger timing: ZoneTriggered, augmentation, ZoneStopped,
  blind duration); `AmbientContextTriggered` and the `AvAnalysis…`
  commands are logged and dropped (no AvAnalysis cluster). Use it with
  `--tls-sim`, which the push transports' TLS endpoints need.
- `--jointfabric-sim` (TESTING ONLY) bridges a JointFabricAdministrator
  (`jointfabric.go`): JointFabricDatastore over the node's store and a
  simulated member-node synchronisation that reaches no node, and
  JointFabricAdministrator with a software administrator key kept in
  `--db`; the flag turns on the node's client stack for the commissioner
  reach.
- `--otbr` serves, besides ThreadBorderRouterManagement, the border
  router's ThreadNetworkDiagnostics with the packet, error, MLE and MAC
  counts and a ThreadNetworkDirectory over the kit's memory store.
- `--light-switch` (TESTING ONLY) bridges an On/Off Light Switch after
  every other device (`light_switch.go`), for the binding certification
  cases the Go controller runs as TH (TC-BIND-2.1, TC-BIND-2.3): its Binding
  server's entries are resolved by the node's binding manager, over the
  client stack the flag turns on, and the app pipe's `{"Name":
  "SendOnOffToBindings", "EndpointId": <switch>, "Command": "on"|"off"}`
  sends OnOff On or Off to every target the entries name, unicast or
  group — matter.js's cert light switch and its `sendOnOffToBindings`
  backchannel. The daemon's default composition does not change.
- `--bridge-sim` (TESTING ONLY) bridges, after every other device, the
  bridge matter.js's bridge test instance simulates after chip's
  bridge-app (`bridge_sim.go`), for the bridge-client certification case
  the Go controller runs as the DUT (TC-BR-4): "Light 1", "TempSensor 1"
  and "TempSensor 2", a battery-powered "Composed Device" (Bridged Node
  and Power Source, PowerSource with the Battery feature) with two
  temperature sensors as its own parts, and "Action Light 1"–"4". The app
  pipe's `{"Name": "toggleBridgedLights"}`, `warmBridgedTemperatureSensors`,
  `renameBridgedLights`, `addBridgedLight` and `removeBridgedLight` — the
  test instance's backchannel names, no further fields — toggle "Light 1"
  (and "Light 2" once added), warm the four sensors by one degree, rename
  the two lights "Light 1b" / "Light 2b", add "Light 2" under a fresh
  endpoint number, and remove "Light 1"; add and remove do nothing when
  already done. The endpoint numbers are the node's own allocation, not
  chip's: the case finds the devices by their NodeLabel. The daemon's
  default composition does not change.
- `--icd-lit` (TESTING ONLY) makes the node a Long Idle Time ICD after
  matter.js's lit-icd test app (`icd.go`), for the ICD client
  certification cases the Go controller runs as the DUT (TC-ICDM-6.1,
  TC-ICDB-1.3): ICD Management on the root with Check-In, User Active Mode
  Trigger, LIT and Dynamic SIT/LIT, SIT until a client registers; a fixed
  cadence of the active window and the idle window, so a registered client
  receives Check-Ins; and, with `--enable-key`, the ICD test event triggers
  (`0x0046000000000001` AddActiveMode … `…04` InvalidateAllCounterValues,
  `…06`/`…07` the Dynamic SIT/LIT ones) on TestEventTrigger.
  `--icd-idle-mode-duration` (seconds, default 3600) and
  `--icd-active-mode-duration-ms` (default 10000) are the app's timings.
  The daemon's default composition does not change.
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

So this example persists, and it does so with one setting: `node.Store.Path`
names the SQLite file (`--db`), and the node does the rest.

- **The node opens the file** (`modernc.org/sqlite`, already a direct
  requirement of `go-fabric`, foreign keys and a busy timeout on, a new
  file created `0600` — it holds the operational private keys) and closes
  it on `Stop`. A host with its own handle or migration tool hands the node
  that instead (`node.Store.DB`).
- **The module owns the DDL.** The node applies the device store's and the
  endpoint store's embedded schemas on every boot, so a schema change
  arrives with the dependency bump and this example writes no SQL.
- **Endpoint numbers only ever advance**, kept under each device's
  `StableKey` (`device.StringKey` here): a device that is removed keeps
  its number for its return, and the Aggregator's `Forget` frees it.
- **Subscriptions persist** (`docs/adr/0008`, as matter.js does): each CASE
  subscription is recorded while it is active, and after a restart
  `node.Start` re-establishes it — it resolves the controller over mDNS,
  opens CASE to it and re-sends the priming report under the old
  subscription id, so the controller carries on without re-subscribing. A
  controller it cannot reach within two seconds recovers on its own.
- **CASE resumption records** persist too, so a controller that reconnects
  after a restart resumes in one round trip; CASE sessions themselves stay
  volatile — Matter treats them so.

Delete `reference-bridge.db` to factory-reset.

## What this example does NOT do

Each of these is real production wiring that the loom daemon carries and this
one omits, listed so the omission is not mistaken for "not needed":

- **Reassembly.** The fleet is fixed: every device is added before the
  node starts, and none is added or removed while it runs (a running
  node's `Aggregator().Add` / `Remove` reassemble).
