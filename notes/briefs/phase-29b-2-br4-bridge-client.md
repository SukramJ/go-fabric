# Brief: phase 29b, wave 2 — the Go controller as DUT: TC-BR-4 against the reference daemon as the bridge TH

Milestone 8 (Proof), phase 29 (`docs/roadmap.md` §"Milestone 8: Proof";
concept v13 §C.1). Branch `m8/phase-29b-2` from `main` after wave 29b-1
landed. One PR, title "chiptool: the Go controller as DUT — TC-BR-4, the
bridge client, runs against the reference daemon's simulated bridge (phase
29b-2)". CHANGELOG under `[Unreleased]` → Added. No release.

Wave 29b-1 added the DUT role to the Go-TH leg (`goCertCase.role`,
`roleDUT`; `goTHPICS` as the controller's own declarations; the "Cases run
with the Go controller as DUT" table of `TestCertifiabilityDocument`). Wave
29a-2 (PR #378) added flagged daemon compositions (`goCertCase.daemonArgs`,
`declare`; `--light-switch`, `examples/reference-bridge/light_switch.go`)
and pipe commands named after matter.js's backchannel (`SendOnOffToBindings`,
`control.go`). Read the 29b-1 brief and PR body, the 29a-2 brief and PR
#378's body (reviewer sections too) and that code first. The read map
`~/.cache/go-fabric-scratch/read-phase29b2-br4.md` has `file:line` for the
TS case (§1), matter.js's bridge test instance (§2), its controller's held
view (§3) and our entry points (§4); it is a map — re-read every line you
cite at 862ad75f (`../matter.js`).

## What TC-BR-4 is (lines verified by the reviewer at 862ad75f)

`support/chip-testing/test/cert/TC-BR-4.test.ts:234-247`: PICS
`MCORE.BRIDGECLIENT`, app `bridge`, `controllerCapabilities:
["sustained-subscription"]`, flavors `["matterjs"]` only (chip's bridge-app
cannot be read wildcard, `:242-245`). The DUT is the controller; the TH is
a bridge the test app simulates after chip's `bridge-app` layout
(`:63-77`: lights 3, 9–12; temperature sensors 4, 5 and the composed
device 6 with its own sensors 7, 8 as parts; nothing at 13 until step 3c
adds the second light). Constants `:93-111`: `LIGHT_1_ENDPOINT` 3,
`ADDED_LIGHT_ENDPOINT` 13, `RENAMED_LABEL` "Light 1b", `ONE_DEGREE` 100,
`NOTICE_WAIT_MS` 20 000, `POLL_MS` 100. "A change reaches the DUT as a
report on the subscription it holds for the whole node" (`:104-109`); a
held value is taken **before** any read, because a read feeds the
controller's own copy (`:215-219`). The TH's changes are the backchannel
commands `toggleBridgedLights` `:454`, `warmBridgedTemperatureSensors`
`:520`, `renameBridgedLights` `:696`, `addBridgedLight` `:756`,
`removeBridgedLight` `:859`; finalizer `decommissionAll` `:927-929`.

matter.js's bridge test instance (`support/chip-testing/src/BridgeTestInstance.ts`):
the endpoint map `:30-43`; the composition `:148-172` (aggregator 1; "Light
1"; "TempSensor 1", "TempSensor 2"; the composed device with two
temperature sensors as **its** parts; "Action Light 1"–"4"); the
backchannel switch `:177-201`; `#toggleLights` `:204-212` (OnOff toggle on
light 1 and light 2, absent one skipped); `#warmTemperatureSensors`
`:215-229` (MeasuredValue + 100 on the four sensors, initial 100 `:50`);
`#renameLights` `:232-243` (NodeLabel "Light 1b" / "Light 2b");
`#addLight2` `:246-251` (no-op when present); `#removeLight1` `:254-260`
(`delete()`, no-op when absent); `#bridgedLight` `:289-295`;
`#composedDevice` `:323-351` (BridgedNode + PowerSource Battery,
BatChargeLevel Ok `:62`, EndpointList the composed endpoint and its two
sensors). The backchannel type:
`packages/testing/src/device/backchannel.ts:134-150` (add and remove are
not inverses).

How matter.js's controller holds the bridge: the sustained wildcard
subscription bootstraps the structure (`InProcessControllerAdapter.ts:2745-2782`);
`clientEndpoints()` `:1630-1649` and `clientAttribute()` `:1651-1665` read
the held copy; a removed endpoint disappears when the **root's** PartsList
no longer names it (`packages/node/src/node/client/ClientStructure.ts:1076-1145`,
the erase on `maybeNumber === 0` `:1107-1141`). Ours mirrors it:
`controller/client/structure.go:578-622` `synchronizePartsList`; held view
`client.Node.Endpoints()` `node.go:355`, `Endpoint(n)` `:368`,
`Endpoint.DeviceType()` `endpoint.go:61`, `Cluster(id)` `:138`, `Parts()`
`:148`, `Cluster.Value(attr)` `cluster.go:374`, `Decoded(attr)` `:398`;
`Node.SubscriptionActive()` `node.go:233`.

## Deliverables

1. **The simulated bridge on the daemon** (TESTING ONLY flag
   `--bridge-sim`, after `--light-switch`: `examples/reference-bridge/
   bridge_sim.go`, `main.go` compose, `fleet.go` `endpoints()` appending
   after every other device; `README.md`). It adds matter.js's bridge
   composition **by meaning, not by number** — endpoint numbers come from
   the node's persisted allocation (`endpoint/identity.go:85`) and cannot
   be chosen; the case finds the devices by their NodeLabel and device type
   (below). Devices, with the same NodeLabels as `BridgeTestInstance.ts:148-172`:
   "Light 1" (On/Off Light), "TempSensor 1", "TempSensor 2" (Temperature
   Sensor, MeasuredValue 100, min −27315, max 32766 — `:48-50`), "Composed
   Device" (`device.NewBridgedNode`, `device/bridged_node_gen.go:65`, with
   the PowerSource server's Battery feature — BatChargeLevel Ok, Status
   Active, Order 0, Description "Battery", BatReplacementNeeded false,
   BatReplaceability NotReplaceable, EndpointList the composed endpoint and
   its parts — and two Temperature Sensor parts via `Device.AddPart`,
   `device/device.go:604`, carrying no BridgedDeviceBasicInformation, as
   `:156-167`; read what `bridged_node_gen.go` declares for PowerSource
   first — `BridgedPowerSourceInfo` conformance, `:41-45` — and say how the
   feature is enabled), "Action Light 1"–"4"; "Light 2" is not in the
   composition until the pipe adds it. The second light's `StableKey` is
   its own, so `Aggregator.Add` gives it a fresh number (`node/aggregator.go:
   245-252`), and `Aggregator.Remove` of "Light 1" keeps its number
   reserved (`:270-280`) — exactly matter.js's add-second/remove-first.
   Keep the device handles in the flag's struct (as `demoLightSwitch` does)
   so the pipe can act on them: `demoLight.setOn` (`fleet.go:135`) for the
   toggle; `TemperatureSensor.TemperatureMeasurement().Set(Patch{MeasuredValue})`
   (`device/temperature_sensor_gen.go:68`, the pattern of `fleet_sensors.go:
   143-153`) for the warm; `Device.Rename` (`device/lifecycle.go:138-148` →
   `node/lifecycle.go:49-55`, `NotifyEndpointNodeLabel`) for the rename;
   the Aggregator (`main.go:392-407` shows how `agg` reaches a closure) for
   add and remove. A reassembly reports PartsList of endpoints 0 and 1 and
   every attribute of an added endpoint (`bridge/bridge.go:1141-1181`); cite
   it at the step that relies on it.
2. **Five pipe commands**, named as matter.js's backchannel names them
   (`control.go` `applyPipeCommand`; `{"Name": "toggleBridgedLights"}` and
   so on, no further fields): each does what the instance's handler does
   (`:204-260`), absent endpoint skipped, add/remove no-ops when already in
   that state. Without `--bridge-sim` each is refused with a message naming
   the flag (as `SendOnOffToBindings` without `--light-switch`). Unit test
   in `bridge_sim_test.go` after `light_switch_test.go`: the composition
   mounts free of device-type violations, the composed device's parts are
   its own PartsList, and each command moves the state it names (the
   toggle, the warm by 100, the rename, add then remove through
   `Aggregator`).
3. **The case** (`gocert_br_test.go`, `roleDUT`, `daemonArgs:
   []string{"--bridge-sim"}`, `declare` with `MCORE.BRIDGECLIENT` and
   `MCORE.DEVLIST.UseDevices/UseDeviceState/UseDeviceName/UseBatInfo` = 1
   as the DUT controller's own statement — the daemon's file says 0 for the
   device, `reference-bridge.pics:47-51`; cite matter.js's controller
   overlay `InProcessControllerAdapter.ts:177-308` if it declares them, else
   mark them ours). The DUT controller is built **with** the default
   subscription (as `gocert_su_test.go:154-171`: `Pair` without
   `AutoSubscribe`, then `waitFor(..., n.SubscriptionActive)`), not with
   `goTH.pair`'s `AutoSubscribe: off` (`gocert_test.go:628-635`). Endpoint
   discovery after commissioning, once, from the DUT's held view: LIGHTS =
   the endpoints whose DeviceTypeList holds 0x0100 and whose
   BridgedDeviceBasicInformation.NodeLabel is one of the five light labels
   (the daemon's own "Desk Lamp" and the other committed lights are not
   the TS's; say so at the step); SENSORS = "TempSensor 1", "TempSensor 2"
   and the two parts of "Composed Device"; COMPOSED = "Composed Device";
   LIGHT_1 = "Light 1". Steps (`TC-BR-4.test.ts` lines from the map §1,
   re-read): 1a `:248-331` wildcard reads of DeviceTypeList and PartsList
   (`goTH.read` with a path without endpoint, `HasEndpoint: false`), every
   expected endpoint answered, the composed one names its parts; 1b
   `:332-381` held device types; 1c `:382-429` held NodeLabel of the named
   devices before the read, equal after; 1d `:430-441` held OnOff before,
   equal after the read; 1e `:442-484` toggle, the held OnOff of LIGHT_1
   flips within 20 s on the subscription alone (poll the held copy every
   100 ms, no read until it flipped), then the checks; 1f `:485-496`; 1g
   `:497-561` warm, all four held MeasuredValues + 100 within 20 s; 1h
   `:562-603` held BatChargeLevel of COMPOSED equals the read; 2a `:604-691`
   Off then On on LIGHT_1, the read says so; 3a `:692-721` rename, held
   NodeLabel "Light 1b" within 20 s; 3b `:722-751`; 3c `:752-830` add, a
   new endpoint appears in `Endpoints()` within 20 s, root and aggregator
   PartsList name it, it answers its own PartsList, its DeviceTypeList holds
   0x0100; 3d `:831-854`; 3e `:855-905` remove, LIGHT_1 leaves `Endpoints()`
   within 20 s, root and aggregator PartsList no longer name it; 3f
   `:906-926`; finalize decommission. The TS's TH-log checks
   (`expectAttributePathIB` `:187`, `expectCommandInvoke` `:636-666`) have
   no daemon-log equivalent unless the daemon logs received paths at debug
   level — check `bridge/receive.go` / `im_gate.go`; if it does not, the
   step's evidence is the read's own result and the note says the log
   check is not mirrored (not a skipped step: the TS's verdict rests on the
   read too). Each step's evidence named at the step (the 29b-1 rule).
   `NOTICE_WAIT_MS` and `POLL_MS` as constants citing `:110-111`.
4. **Status**: golden `goCert` entry for BR/4 with `role`; the status page's
   BR/4 row leaves class (c) for the DUT table (delete the `"BR/4"` entry of
   the `controllerRoleCases` map, `internal/chiptool/families_table_test.go:46`,
   whose reason says "phase 29b-2"); `docs/roadmap.md` phase 29 progress line;
   `docs/certifiability.md` prose; `internal/chiptool/doc.go`; CHANGELOG.
   A step that fails on the controller side is a findings-register bullet
   with `goStep.finding` (the DUT is `controller/client`: no fix in this
   wave beyond one obvious line, which the reviewer re-reads); on the
   daemon side the same rule as 29a-2.

## Rules

As 29b-1's brief: author identity before every commit (`SukramJ
<sukramj@icloud.com>`), no trailers; guards under the flock with
`PATH=$HOME/go/bin:$PATH`, logs `~/.cache/go-fabric-scratch/p29b2/`
(summary log with HEAD before/after), grep not cat; the chip-tool harness
alone under the flock; read slices; cite at 862ad75f; no security path is
edited; guards `make ci` (whole, to the end — `cover-check` runs after
lint, so a lint failure hides a floor failure: rerun after every fix),
race on `./internal/chiptool/... ./controller/client/... ./node/...
./examples/reference-bridge/...`, `make chiptool-gocert`, `make
chiptool-test`, `TestCertifiabilityDocument`, `cite-check`,
`reachability-check`, `apidiff-check`, `mkdocs build --strict`; no family
run (`--bridge-sim` is off by default, the committed slices do not move).
Every new test file moves the citation and reachability snapshots: run
`make cite-check` and `make reachability` last and commit what they
regenerate. Rebase onto `origin/main` before the PR; PR without
auto-merge; report in the fixed shape (per §, guards with HEAD,
differences, decisions, left out, open questions, by_design candidates,
every citation listed).
