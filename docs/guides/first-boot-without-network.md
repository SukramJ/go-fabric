# A node without a network at first boot

A device that leaves the box has no network yet: no cable, no Wi-Fi
credentials. Matter commissions such a device over Bluetooth LE — the
commissioner finds it by its BLE advertisement, runs PASE over BTP, hands it
the Wi-Fi credentials through NetworkCommissioning, and completes
commissioning over CASE on the Wi-Fi network the device joined. This page
ties together the two host ports a node needs for it and the example that
composes them.

## The two ports

| Port | Field | Linux implementation | What it does |
| --- | --- | --- | --- |
| `contract.BLEPeripheral` | `node.Config.Network.BLE` | `ble/bluez` (nested module) | the Matter GATT service and the advertisement on the host's adapter, through bluetoothd |
| `contract.WiFiDriver` | `node.Config.Network.WiFi.Driver` | `wifi/wpasupplicant` (nested module) | scan and join through wpa_supplicant's D-Bus interface; the node then serves NetworkCommissioning with the Wi-Fi feature |

Both are nested modules: a host without a radio never pulls godbus. The
radio side and the network side are documented in the operations guide,
[BLE commissioning](./operations.md#ble-commissioning) and
[Wi-Fi provisioning](./operations.md#wi-fi-provisioning).

```go
radio, err := bluez.NewPeripheral("hci0")
if err != nil {
	return err
}
defer radio.(io.Closer).Close()
wifi, err := wpasupplicant.New(ctx, wpasupplicant.Options{Interface: "wlan0"})
if err != nil {
	return err
}
defer wifi.Close()

n, err := node.New(ctx, node.Config{
	Network: node.Network{BLE: radio, WiFi: &node.WiFi{Driver: wifi}},
	// …
})
```

## What happens, in order

| Phase | The node |
| --- | --- |
| first start, no fabric | opens its own commissioning window, advertises it over BLE (`MATTER-<discriminator>`), serves one central; its QR code names BLE discovery |
| PASE over BTP | the commissioner arms the fail-safe, reads the node, adds the certificates |
| AddOrUpdateWiFiNetwork, ConnectNetwork | the driver joins the network; on Success the node re-announces its DNS-SD records, and the mDNS responder joins the Wi-Fi interface as it comes up |
| CASE over Wi-Fi, CommissioningComplete | the network is committed into the node's store; the node leaves the radio (the advertisement stops, the central is disconnected, the GATT service is unregistered) |
| every later start | the committed network is rejoined before the fabrics' records go out; the node does not touch the radio |
| last fabric removed | the own window opens again and is advertised over BLE |

The responder needs no interface at start: when none is up, its sockets
stay open and join each interface as it comes up, so a node that starts
before its Wi-Fi interface is associated is still found on that network
afterwards.

## The example

[`examples/ble-wifi-light`](https://github.com/SukramJ/go-fabric/tree/main/examples/ble-wifi-light)
is one on/off light that composes both ports. It is a module of its own,
built from a clone:

```sh
cd examples/ble-wifi-light
go build -o ble-wifi-light .
sudo ./ble-wifi-light --adapter hci0 --wifi-interface wlan0 --storage light.db --log-level debug
```

It prints the QR code and the manual code. Commission it with the QR code —
the manual code names no discovery capability, so a commissioner may not
look for it over BLE:

```sh
chip-tool pairing ble-wifi 1 <ssid> <passphrase> 20202021 3840
reference-controller pair --ble --ble-adapter hci0 --code '<QR code>' --wifi-ssid <ssid> --wifi-credentials <passphrase>
```

`--no-wifi` leaves the Wi-Fi driver out: the node serves the Ethernet
NetworkCommissioning and is reached over the host's present network, still
commissioned over BLE — for a host or a test without a Wi-Fi radio.

The host's part — bluetoothd and wpa_supplicant running, NetworkManager off
the Wi-Fi interface, the process allowed on the system bus — is in the
operations guide.

## What proves it

- the in-process tests: the example's node on the in-memory BLE radio, the
  node's BLE and Wi-Fi tests, and the fake-bus tests of both modules;
- `make chiptool-ble-wifi` (and its CI job): the CSA's chip-tool commissions
  the example over BLE onto Wi-Fi under connectedhomeip's own BlueZ and
  wpa_supplicant mocks, then reads NetworkCommissioning and switches the
  light, with chip's all-clusters app under the same mocks as the control;
- the weekly `ble-virtual` job: the example on real bluetoothd over virtual
  HCI controllers, commissioned by the module's controller and by
  matter.js's, its Wi-Fi a real wpa_supplicant with hostapd on
  mac80211_hwsim radios where the runner's kernel has the module;
- the owner's hardware test on a Raspberry Pi with chip-tool, the reference
  controller and Apple Home.

The certification cases that commission over BLE inside the test do not
run: the harness's Python controller is built without BLE
([certifiability](../certifiability.md)).

*Source: [`examples/ble-wifi-light/main.go`](https://github.com/SukramJ/go-fabric/blob/main/examples/ble-wifi-light/main.go),
[`node/config.go`](https://github.com/SukramJ/go-fabric/blob/main/node/config.go) (`Network.BLE`, `WiFi.Driver`),
[`mdns/socket.go`](https://github.com/SukramJ/go-fabric/blob/main/mdns/socket.go) (`openSockets`, `joinNew`),
[`script/ble-virtual-test.sh`](https://github.com/SukramJ/go-fabric/blob/main/script/ble-virtual-test.sh).*
