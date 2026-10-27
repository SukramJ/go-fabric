# Commissioning through ESPHome Bluetooth proxies

The commissioner reaches a device that is still on BLE through a
`controller/ble.Central` ([ADR 0024](../adr/0024-ble-commissioning-in-the-controller.md)).
Besides the BlueZ platform, the sub-module
[`ble/esphome`](https://github.com/SukramJ/go-fabric/tree/main/ble/esphome)
provides one whose radios are **ESPHome Bluetooth proxies**: ESP32 boards on
the network that scan and hold GATT connections on the host's behalf, reached
over the ESPHome native API on TCP. The host needs no Bluetooth adapter and
no D-Bus; the module is pure Go and builds for every platform.

Neither matter.js nor connectedhomeip commissions through a proxy; the
central runs the same sequence as the BlueZ one, spoken in the API's
messages (the module's package documentation cites them).

## What a proxy needs

- **`bluetooth_proxy: active: true`.** Connections exist only with it; a
  proxy whose feature flags lack `ACTIVE_CONNECTIONS` (or `REMOTE_CACHING`,
  ESPHome before 2022.12) is refused by `NewCentral` with
  `esphome.ErrNoActiveConnections` (`ErrNoRemoteCaching`).
- **An API key**, `api: encryption: key:` — base64 of 32 bytes. The central
  speaks the API's Noise transport with it; without a key the API is
  plaintext. A wrong key (`esphome.ErrInvalidKey`), a key at a plaintext
  proxy (`ErrPlaintextDevice`) and none at an encrypted one
  (`ErrRequiresEncryption`) fail `NewCentral` with the texts Home Assistant
  shows for them.
- The proxy's **API port**, 6053 by default.

## One subscriber per proxy

A proxy forwards advertisements, and answers GATT requests, to exactly one
API client: the last one that subscribed (ESPHome's `bluetooth_proxy`
component, "the newest subscriber wins"). Home Assistant's Bluetooth
integration is usually that client. While the central scans or holds a
connection it is the subscriber, and Home Assistant loses the proxy until
it subscribes again — which takes the proxy back from the central.

The central logs `ble.esphome.proxy_taken` whenever it subscribes, and does
not fight: a subscribed proxy that forwards no advertisement for
`Options.SilenceTimeout` (30 s) is presumed taken; the central reconnects
once after the backoff, and when the proxy falls silent again it is busy —
`esphome.ErrProxyBusy` — until the next scan starts afresh. Use a proxy
dedicated to commissioning, or accept that Home Assistant's use of it
flaps while a device is paired.

## Wiring

```go
central, err := esphome.NewCentral([]esphome.Proxy{
	{Addr: "proxy-office.local", Key: officeKey},
	{Addr: "10.0.0.42:6053", Key: hallKey, Name: "hall"},
}, esphome.Options{Logger: log})
if err != nil {
	return err // names the proxy that refused
}
defer central.Close()

node, err := ctl.Pair(ctx, code, controller.PairOptions{
	BLE:  central,
	WiFi: &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
})
```

`NewCentral` makes each proxy's first connection: a proxy that refuses fails
it, and so does every proxy being unreachable; one that is only unreachable
while another answers is retried in the background. Scanning runs on every
proxy. A device two proxies see is reported by both, and the connect goes
through the proxy that last saw it with the strongest signal. A proxy that
drops is reconnected with backoff (one second, doubling to a minute); a BTP
session through it ends with `btp.ErrDisconnected`. The central implements
`ble.Reachability`: a device is a candidate while a connected, non-busy proxy
reports it and has a connection slot for it.

## The reference controller

`ble/esphome/cmd/reference-controller` is the reference controller with
proxies. `pair` takes `--bluetooth-proxy host[:port][,key]`, repeatable,
which implies `--ble`:

```sh
cd ble/esphome && go build -o reference-controller ./cmd/reference-controller
./reference-controller pair --bluetooth-proxy proxy-office.local,$KEY \
  --code MT:-24J0AFN00KA0648G00 --wifi-ssid lab --wifi-credentials secret12
```

## Testing

`ble/esphome/esphometest` is an in-process proxy speaking the API over a
loopback listener, plaintext or Noise, in front of `controller/ble/bletest`'s
peripherals. The commissioner's BLE tests run against it unchanged but for
the central.
