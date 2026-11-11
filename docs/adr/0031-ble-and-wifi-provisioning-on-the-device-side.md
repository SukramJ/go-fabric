# ADR 0031 — BLE and Wi-Fi provisioning on the device side

- **Status**: Accepted (owner, 2026-11-11; roadmap milestone 7, phase 28)
- **Date**: 2026-11-11
- **Supersedes**: decision 1 of
  [ADR 0024 — BLE commissioning in the controller role](./0024-ble-commissioning-in-the-controller.md)
  ("the device role stays without Bluetooth")
- **Related**: [ADR 0018 — device layer and node facade](./0018-device-layer-and-node-facade.md),
  [ADR 0026 — architecture principles](./0026-architecture-principles.md),
  `docs/matterjs-comparison.md` (the BLE row and non-goal 2)
- **Implementation**: waves 28a (the BLE transport and the BlueZ
  peripheral), 28b (the Wi-Fi driver and the facade's Wi-Fi field) and 28c
  (the proof); one release, `v0.22.0`

## Context

"No Bluetooth on the device side" was a scope boundary since concept v1,
kept by ADR 0024 when the commissioner learned BLE: the module's first
hosts are LAN-attached bridges, and a bridge that already has an address
is found over DNS-SD. A node that is not a bridge — a light, a sensor, a
plug on a Linux board without a cable — has no address when it is first
powered. Matter's answer is commissioning over Bluetooth LE: the
commissioner finds the node by its BLE advertisement, runs PASE over BTP,
hands it the Wi-Fi credentials through NetworkCommissioning, and meets it
again on the IP network. Without that path the `node` and `device` facade
can state such a device but nobody can commission it.

matter.js has the path: a peripheral on bleno
(`packages/nodejs-ble/src/BlenoBleServer.ts`), the BLE advertiser
(`packages/protocol/src/advertisement/ble/`), and a node runtime that is
on BLE only while it has no fabric
(`ServerNetworkRuntime.ts:255-275`, `:294-308` at `db021da0`). It has no
Wi-Fi driver: `NetworkCommissioningServer` is an empty class
(`NetworkCommissioningServer.ts:18`) and its example fakes the join
(`examples/device-onoff-advanced/src/cluster/DummyWifiNetworkCommissioningServer.ts`).
connectedhomeip's Linux platform has both, over BlueZ's and
wpa_supplicant's D-Bus interfaces (`src/platform/Linux/bluez/`,
`ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp` at
`6170af84`).

This module already had the pieces below the radio: `transport/btp` with
both roles, a transport-neutral receive path that serves TCP beside UDP,
and the Wi-Fi NetworkCommissioning server after connectedhomeip
(`cluster/core`), reachable through a test seam only.

## Decision

1. **A node without a fabric is commissionable over BLE; a node with one
   is never on the radio.** The node advertises for its own window — the
   factory window and one its host opens — while it has no fabric, and
   serves PASE over BTP. When that window's time is up the advertisement
   ends and the GATT service stays, so a commissioning in flight
   finishes; the node leaves the radio at CommissioningComplete and at
   `Stop`, and returns to it when its last fabric is removed. A window an
   administrator opens on a commissioned node is announced over DNS-SD
   only. One central at a time. This is matter.js's rule, taken whole
   (`BleAdvertisement.ts:69-71`, `ServerNetworkRuntime.ts:255-275`,
   `:294-308`).
2. **Both radios are host ports in `contract`.** `contract.BLEPeripheral`
   (advertise, serve the Matter GATT service, one `BLEConnection` per
   central) stands beside `contract.WiFiDriver` (scan, connect). The
   module owns everything above them — BTP, the advertisement's payload
   and schedule, the window's lifecycle, the NetworkCommissioning rules
   and the store — and nothing below.
3. **The BTP peripheral server is platform-independent**
   (`transport/btp.Accept`): the handshake held until the C2
   subscription, the 15 s timer, the single session, the indication's
   confirmation. matter.js has this logic inside its bleno binding; here
   every platform and the in-memory radio of the tests share one
   implementation, tested against `BlenoBleServer.ts` line by line.
4. **The facade states a wireless node in two fields**:
   `node.Network.BLE` and `node.Network.WiFi`. With BLE the onboarding
   payload carries the BLE discovery bit. Wi-Fi commissioning leaves the
   test seam and becomes the facade's.
5. **The platforms are nested modules, pure Go, Linux**: `ble/bluez`
   gains the GATT peripheral beside its central; `wifi/wpasupplicant`
   is the Wi-Fi driver. Both follow connectedhomeip's D-Bus plumbing —
   matter.js's peripheral speaks raw HCI through a native binding, and
   it has no Wi-Fi driver — over `github.com/godbus/dbus/v5`, without
   cgo. The main module keeps its dependency list; a wired host pulls
   neither.
6. **The proof is automated before the release**: the platforms' tests
   over in-process buses on every pull request, real BlueZ over virtual
   controllers in both directions weekly, and a chip-tool `ble-wifi`
   commissioning with the DD and Wi-Fi CNET cases over connectedhomeip's
   BlueZ and wpa_supplicant mocks. The hardware run (a Linux board
   without a cable, Apple Home) follows the release; what it finds is a
   patch release (owner decision 2026-11-11).

**No Thread radio** stays a scope boundary: a Thread node needs an 802.15.4
radio and a Thread stack, neither of which is a host port of this shape.

## Security

BLE widens who can reach an uncommissioned node from the local network
to everyone in radio range. What they reach is PASE: the link is not
paired or encrypted by BlueZ, and the module does not rely on it; the
session is as secure as the passcode, as over UDP. The limits are the
window's — the PASE attempt limit and the window's lifetime — and the
radio's: one central, a handshake that must complete within 15 s of its
request. A central that connects and never sends a handshake request
holds the one slot until it disconnects; matter.js and connectedhomeip
have the same property, and a device in radio range can deny service to
a BLE commissioning by simpler means. A commissioned node is not on the
radio at all. The Wi-Fi credential arrives inside the PASE session, is
stored once, in the node's store, and is handed to wpa_supplicant only
to join; it is never logged.

## Better than the others, than before, the right cut

- **Than matter.js**: no native build and no exclusive HCI channel — the
  adapter stays bluetoothd's and is shared with the host's other uses; a
  working Wi-Fi driver instead of an example that fakes the join; the
  BTP server separated from the platform (matter.js's own TODO at
  `packages/protocol/src/ble/Ble.ts:24`).
- **Than connectedhomeip**: the same D-Bus calls without GLib and cgo,
  behind two small ports instead of a platform singleton; the Wi-Fi
  credential in one store, not also in wpa_supplicant's configuration.
- **Than before**: a device that is not a bridge can be commissioned at
  first power-on; the tests' BLE route is the production route.
- **The cut**: Matter's rules in the module, radios behind `contract`,
  platforms in modules of their own, the host choosing which it links.

## Consequences

- ADR 0024 keeps its decisions 2–5; its decision 1 ends here.
  `CLAUDE.md`'s scope boundaries name the Thread radio only.
- `ble/bluez` and `wifi/wpasupplicant` are tagged with every release of
  the main module.
- The DD and SC BLE cases and the Wi-Fi CNET cases move from "out of
  scope" to covered in `docs/certifiability.md` as their legs land.
- A deliberate difference from matter.js or connectedhomeip on this path
  is a `by_design` entry (`BD-Matter-BLEPeripheralOverBlueZ` is the
  first).
