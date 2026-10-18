# `ble/bluez` — BlueZ for the commissioner's BLE seam

`github.com/SukramJ/go-fabric/ble/bluez` implements `controller/ble.Central`
and `controller/ble.Connection` over BlueZ on the D-Bus system bus, in pure Go
(no cgo). With it, `controller/ble.NewScanner`, `controller/ble.Dial` and
`commissioner.Options.Channel` commission a Wi-Fi or Thread device over BLE
([ADR 0024](https://github.com/SukramJ/go-fabric/blob/main/docs/adr/0024-ble-commissioning-in-the-controller.md)).
The device role of go-fabric never uses Bluetooth.

It is a Go module of its own, so the main module keeps its dependencies and a
host without BLE never pulls `github.com/godbus/dbus/v5`. The main module
never imports it.

```go
central, err := bluez.NewCentral("hci0") // "" is hci0
if err != nil {
	return err // an *bluez.Error names the D-Bus error
}
defer central.(io.Closer).Close()
devs, err := ble.NewScanner(central, ble.ScannerOptions{}).Find(ctx, id, 10*time.Second, false)
…
ch, err := ble.Dial(ctx, central, devs[0].Peripheral, ble.DialOptions{})
```

## Versions

`go.mod` requires the main module at the release this module ships with and
replaces it with the checkout beside it (`replace … => ../..`) for in-repo
builds. A release of this module (tag `ble/bluez/vX.Y.Z`) moves with the main
module's version and bumps that require to the main module's tag it ships
with; a consumer outside this repository gets the required tag, since a
`replace` applies only inside this module.

## What it speaks

The sequence is matter.js's (`packages/nodejs-ble/src/NobleBleClient.ts`,
`NobleBleChannel.ts`); the D-Bus calls are BlueZ's documented API
(https://github.com/bluez/bluez/tree/master/doc):

| Step | BlueZ |
| --- | --- |
| scan for the Matter service, every advertisement | `org.bluez.Adapter1.SetDiscoveryFilter` (`Transport: "le"`, `DuplicateData: true`; no `UUIDs`, which BlueZ turns into the kernel's service discovery that drops a Matter advertisement without a UUID field), `StartDiscovery`, `StopDiscovery`; the Matter service data selects |
| advertisements | `InterfacesAdded` and `PropertiesChanged` of `org.bluez.Device1`, `ServiceData[0000fff6-…]` (8 bytes) |
| connect | `org.bluez.Device1.Connect`, then `ServicesResolved` (bounded by 2 min and 1 min, as matter.js bounds noble) |
| GATT | the `org.bluez.GattService1` with the Matter UUID; its `GattCharacteristic1` C1 (`WriteValue`, type `request`), C2 (`StartNotify`, values as `PropertiesChanged` of `Value`), C3 (`ReadValue`, optional) |
| ATT_MTU | C1's `MTU` property where BlueZ offers it, else the BTP minimum |
| disconnect | `StopNotify` on C2, `org.bluez.Device1.Disconnect` (each bounded by 5 s); `Connected=false` or the device's removal ends the connection |

## Rights

BlueZ answers only callers its D-Bus policy admits. On most distributions
that is root and members of the `bluetooth` group
(`/usr/share/dbus-1/system.d/bluetooth.conf`); elsewhere a polkit rule or a
policy file for `org.bluez` grants it. Without the right the first call fails
with `org.freedesktop.DBus.Error.AccessDenied`, and the error says so:

```
bluez: org.freedesktop.DBus.ObjectManager.GetManagedObjects on /: org.freedesktop.DBus.Error.AccessDenied: …
```

An adapter that is off answers `org.bluez.Error.NotReady`
(`bluetoothctl power on`); a missing adapter is `bluez.ErrNoAdapter`.

## Platforms

Linux only. Elsewhere `NewCentral` returns `bluez.ErrUnsupported`: macOS
and Windows need cgo or COM for their Bluetooth stacks (ADR 0024 decision 6).

## Licences

Apache-2.0 ([`LICENSE`](./LICENSE), [`NOTICE`](./NOTICE)). The dependency it
adds, `github.com/godbus/dbus/v5` (BSD-2-Clause), is recorded in
[`THIRD-PARTY-NOTICES.md`](./THIRD-PARTY-NOTICES.md) with its licence text
under [`licenses/`](./licenses/).
