# `wifi/wpasupplicant` — the node's Wi-Fi driver on Linux

`github.com/SukramJ/go-fabric/wifi/wpasupplicant` implements
`contract.WiFiDriver` over wpa_supplicant's D-Bus interface
(`fi.w1.wpa_supplicant1` on the system bus), in pure Go (no cgo). Set it as
`node.Config.Network.WiFi.Driver`: the node then serves NetworkCommissioning
with the Wi-Fi feature and joins the network a commissioner provisions —
typically over BLE (`node.Config.Network.BLE`, `ble/bluez`).

It is a Go module of its own, so the main module keeps its dependencies and a
wired host never pulls `github.com/godbus/dbus/v5`. The main module never
imports it, and it does not import `ble/bluez`.

```go
wifi, err := wpasupplicant.New(ctx, wpasupplicant.Options{Interface: "wlan0"}) // "" is wlan0
if err != nil {
	return err // off Linux: errors.Is(err, errors.ErrUnsupported)
}
defer wifi.Close()
cfg.Network.WiFi = &node.WiFi{Driver: wifi}
```

matter.js ships no Wi-Fi driver, so the driver follows connectedhomeip's Linux
platform (`src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp`
at 6170af84): the interface by `GetInterface` or `CreateInterface`, auto scan
off; an active scan and its BSSs; the join as `RemoveNetwork` of the current
network, `AddNetwork`, `Disconnect`, `SelectNetwork`, and its verdict from the
interface's `State`, with eight retries on an association timeout.

The host runs wpa_supplicant with its D-Bus interface, keeps the interface out
of NetworkManager's hands, grants the process the system bus's permission for
`fi.w1.wpa_supplicant1`, and does the addressing: the driver saves nothing into
wpa_supplicant's configuration (the node's store keeps the committed network)
and runs no DHCP client. The Wi-Fi credential is never logged.

`make wpasupplicant-check` builds (Linux and the `!linux` stub), vets, tests
(over an in-process bus that plays wpa_supplicant) and lints the module; the
[operations guide](https://github.com/SukramJ/go-fabric/blob/main/docs/guides/operations.md#wi-fi-provisioning)
has the host's checklist.
