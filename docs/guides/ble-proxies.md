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

The silence counts advertisements of any kind, so a proxy in a BLE-quiet
place — a shielded lab, a remote site with no other Bluetooth devices — is
presumed taken even when nobody else uses it. When the log shows
`ble.esphome.proxy_busy` and no other client holds the proxy, raise
`Options.SilenceTimeout`.

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

## Discovery and keys per device

A proxy does not have to be listed. ESPHome devices advertise their API over
DNS-SD as `_esphomelib._tcp.local` on the API port, named after the device
(`kitchen-proxy`), with TXT keys such as `version`, `mac` (twelve hex
digits), `config_hash`, `board`, and `api_encryption` when the device has an
API key. Whether a device is a Bluetooth proxy is not in that record: it is
in the feature flags the device reports once connected. `esphome.Discover`
therefore browses for a while, connects to each device once, and returns the
ones with active connections:

```go
names, err := discovery.Open(log) // client/discovery: the commissioner's DNS-SD cache
if err != nil {
	return err
}
defer names.Close()
keys := esphome.Keys{
	"kitchen-proxy":     kitchenKey, // by the device's name …
	"AA:BB:CC:DD:EE:FF": hallKey,    // … or by its MAC
}
proxies, err := esphome.Discover(ctx, names, keys, 3*time.Second)
```

Keys are per device, by name or by MAC (any spelling; a name matches first).
A device without a key is used only when it answers plaintext; one that
advertises `api_encryption` without a key in `Keys` is not dialled at all.
Every device left out — no key, a wrong key, no `bluetooth_proxy: active:
true`, unreachable — is logged as `ble.esphome.proxy_skipped` with the
reason.

A central keeps the discovered set current when `Options.Discover` is set:

```go
central, err := esphome.NewCentral(nil, esphome.Options{Logger: log,
	Discover: &esphome.DiscoverOptions{Browser: names, Keys: keys}})
```

It starts with the proxies it is given and the ones it finds, browses again
every `Rescan` (five minutes) and after every proxy loss, and adds or drops
proxies without touching a running connection: a new proxy joins
(`ble.esphome.proxy_discovered`), a known one whose address changed — a DHCP
lease — is followed by its name after the loss (`ble.esphome.proxy_moved`),
and one no longer advertised leaves once it has no connection
(`ble.esphome.proxy_removed`). A proxy given in the list stays regardless,
and a device advertised at its address is that proxy, not a second one. A
device refused for good (a wrong key, no active connections) is dialled
again only when its `config_hash` changes. `NewCentral` fails when no proxy,
given or discovered, answers.

The one-subscriber rule is unchanged: a discovered proxy is taken from Home
Assistant exactly as a listed one is.

## The reference controller

`ble/esphome/cmd/reference-controller` is the reference controller with
proxies. `pair` takes `--bluetooth-proxy host[:port][,key]`, repeatable,
which implies `--ble`; `--bluetooth-proxy-discover` finds the proxies over
DNS-SD as well, with `--bluetooth-proxy-key name=key` (repeatable, the name
a device name or a MAC) for their keys:

```sh
cd ble/esphome && go build -o reference-controller ./cmd/reference-controller
./reference-controller pair --bluetooth-proxy proxy-office.local,$KEY \
  --code MT:-24J0AFN00KA0648G00 --wifi-ssid lab --wifi-credentials secret12
./reference-controller pair --bluetooth-proxy-discover \
  --bluetooth-proxy-key kitchen-proxy=$KEY \
  --code MT:-24J0AFN00KA0648G00 --wifi-ssid lab --wifi-credentials secret12
```

## Testing

`ble/esphome/esphometest` is an in-process proxy speaking the API over a
loopback listener, plaintext or Noise, in front of `controller/ble/bletest`'s
peripherals. Its `Network` is an in-memory DNS-SD network the fake proxies
advertise on as ESPHome devices do; a `discovery.Names` on `Network.Conn()`
browses them. The commissioner's BLE tests run against it unchanged but for
the central — once with the proxy listed, once discovered. A
`fabrictest.Device`'s `BLE()` is the peripheral to put behind the fake proxy
([Testing in process](testing-in-process.md#ble)).
