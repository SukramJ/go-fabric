# Operations

This page is what a host daemon minds when it runs a go-fabric node: the
files the node keeps, the ports and the multicast it needs, the log lines
and probes that matter in operation, how to read a failed pairing, what a
reset removes and what an upgrade can move. It is written from the node's
side. How the daemon is packaged — a container, a service unit, an add-on —
is the daemon's decision; the node needs the files, ports and capabilities
below, wherever it runs. The [cookbook](embed-in-a-daemon.md) shows them in
a real daemon.

## Files

The node keeps everything it must remember in one SQLite file when the host
sets `node.Config.Store.Path`: it opens the file itself (creating it on
first use) with foreign keys on and a 5 s busy timeout, and closes it on
`Stop`. It sets no journal mode, so SQLite's default rollback journal
applies: a `<file>-journal` next to the database exists while a write is in
progress, and belongs to it.

What the file holds is listed table by table in the
[persistence guide](persistence.md#the-device-database): the fabrics with
their operational certificates and keys, the access control lists, group
keys, the CASE resumption records, the subscriptions to re-establish, the
endpoint numbers of the bridged devices, the node's UniqueID. Losing the
file is a factory reset seen from every controller: the node starts without
a fabric, opens its commissioning window again, and every controller has to
pair it anew, and the bridged devices come back under endpoint numbers the
controllers' automations do not know.

A factory reset on purpose is `Node.FactoryReset`: it removes every fabric
the way RemoveFabric does (each controller's Leave event, flushed with what
its subscriptions owe in one report per subscription and awaited at most
500 ms, its sessions and subscriptions), stops the node and erases its
store and endpoint records (`endpoint.Store.EraseEndpoints`, which restarts
the numbering); the host then builds a new node on the same store, which
starts uncommissioned and numbers its bridged endpoints from 2, as a
fresh install does. A controller's counterpart is `Controller.Erase`,
which drops its paired nodes, its fabric and its CA key material. While
a host hook still holds a node's report queue past the context, it erases
nothing and returns the context's error; called again once the hook
returned, it completes the reset.

So the operator's rules are the persistence guide's
[backup rules](persistence.md#backup):

- keep the file on storage that survives the daemon's restarts and
  upgrades (a volume, the add-on's data directory, the service's state
  directory);
- protect it like a key: it holds the node's private operational keys
  unencrypted. A file the node creates through `Store.Path` (or
  `store.OpenSQLite`) is created with mode `0600`, and
  SQLite gives its `-wal` and `-shm` files the same mode; an existing file
  keeps its mode, and a host that opens its own handle (`Store.DB`) sets the
  mode itself;
- back it up consistently — stop the node, or copy it with SQLite's own
  online mechanisms (`VACUUM INTO`, the backup API), never a plain copy of
  the file under a running node;
- run one copy at a time: a restored file is the same node, and two running
  copies answer as one node under one identity.

*Source: [`node/config.go`](https://github.com/SukramJ/go-fabric/blob/main/node/config.go) (`Store`),
[`store/sqlite.go`](https://github.com/SukramJ/go-fabric/blob/main/store/sqlite.go) (`sqlitePragmas`, `OpenSQLite`), [`node/store_path.go`](https://github.com/SukramJ/go-fabric/blob/main/node/store_path.go) (`openStorePath`),
[`node/node.go`](https://github.com/SukramJ/go-fabric/blob/main/node/node.go) (`Start`: the own window of a node without a fabric).*

## Ports and addresses

| What | Default | Why |
| --- | --- | --- |
| UDP `node.Config.Network.Listen` | `:5540` | Every Matter message — commissioning, operational sessions, reports — arrives here. |
| TCP on the same port | off | Only with `Network.TCP.Incoming`: the node then also accepts Matter over TCP and says so in its session parameters, and the TCP listener takes the UDP socket's port. It holds at most `Network.TCP.MaxInboundConnections` connections in all (zero is 4; see the limits guide). |
| IPv6 on the LAN | required | Matter's operational network is IPv6: the node advertises its IPv6 addresses and controllers reach it there. `Network.PreferIPv4` forces an IPv4-only socket, for a test network. |
| UDP 5353, multicast `224.0.0.251` and `ff02::fb` | required | DNS-SD over mDNS, below. |

A firewall in front of the node lets in UDP 5540 (and TCP 5540 with TCP on)
and mDNS from the controllers' LAN. A container sees the LAN's multicast
only with host networking or an equivalent.

*Source: [`node/config.go`](https://github.com/SukramJ/go-fabric/blob/main/node/config.go) (`Network`),
[`bridge/tcp_listen.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/tcp_listen.go) (`TCPConfig`, `openTransports`).*

## BLE commissioning

A device without a network at first boot is commissioned over Bluetooth LE:
the commissioner finds it by its BLE advertisement, runs PASE over BTP,
provisions the network (Wi-Fi credentials through NetworkCommissioning) and
completes over CASE on the IP network. The node gets the radio as a host
port, `node.Config.Network.BLE` (a `contract.BLEPeripheral`); on Linux that
is `ble/bluez`, a nested module a host without BLE never pulls:

```go
radio, err := bluez.NewPeripheral("hci0") // "" is hci0
if err != nil {
	return err // an *bluez.Error names the D-Bus error
}
defer radio.(io.Closer).Close()
cfg.Network.BLE = radio
```

| When | On the radio |
| --- | --- |
| uncommissioned, its own window open (at start, after the last fabric is removed, or `Node.OpenBasicWindow`) | the GATT service is registered and the window advertised — 20 ms for 30 s, 150 ms until 15 minutes, then the extended announcement (no vendor and product id) at 1285 ms; one central at a time |
| the window closes: its timeout, `Node.CloseWindow` | the advertisement stops; the service stays, and so does a connected central that completed PASE, so a commissioning over BLE in progress completes over the same link; a central that has not completed PASE is disconnected, so it does not hold the one BLE slot into the next window; a new own window advertises again on the same service |
| commissioned: CommissioningComplete commits the fail-safe | the advertisement stops, the central is disconnected, the service unregistered — after the response, not before it |
| commissioned | nothing: no advertisement, no service, also for a window an administrator opens |
| `Node.Stop` | off the radio |

The QR code of `Node.SetupPayload` names BLE discovery beside the IP
network only with `Network.BLE` set; the manual code carries no discovery
bits. BTP is reliable, so MRP does not run over it; a lost BLE connection
closes the sessions it carried.

`ble/bluez` speaks BlueZ's D-Bus API and shares the adapter with
bluetoothd. It needs the system-bus rights its central needs (the
`bluetooth` group on most distributions) and an adapter that is powered on.
The reference daemon has no BLE flag: `examples/ble-wifi-light` is the
example node with BLE and Wi-Fi.

*Source: [`node/commissioning.go`](https://github.com/SukramJ/go-fabric/blob/main/node/commissioning.go) (`followOnBLE`, `leaveBLE`),
[`bridge/ble.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/ble.go) (`StartBLE`, `WithdrawBLE`, `StopBLE`, `advertiseBLE`),
[`transport/btp/accept.go`](https://github.com/SukramJ/go-fabric/blob/main/transport/btp/accept.go) (`Accept`),
[`ble/bluez/peripheral_linux.go`](https://github.com/SukramJ/go-fabric/blob/main/ble/bluez/peripheral_linux.go) (`NewPeripheral`).*

## Wi-Fi provisioning

The network a commissioner provisions over BLE is joined through the
node's Wi-Fi port, `node.Config.Network.WiFi.Driver` (a
`contract.WiFiDriver`): with it the node serves NetworkCommissioning with
the Wi-Fi feature in place of the Ethernet one. On Linux the driver is
`wifi/wpasupplicant`, a nested module a wired host never pulls:

```go
wifi, err := wpasupplicant.New(ctx, wpasupplicant.Options{Interface: "wlan0"}) // "" is wlan0
if err != nil {
	return err
}
defer wifi.Close()
cfg.Network.WiFi = &node.WiFi{Driver: wifi}
```

| When | What happens |
| --- | --- |
| ScanNetworks | an active scan; the results carry SSID, BSSID, channel, band, RSSI and the security bitmap. The node advertises ScanMaxTimeSeconds 10, as connectedhomeip; the driver waits for wpa_supplicant's end of the scan up to 30 s (`DefaultScanTimeout`), since a scan while the interface is associated can take about 10 s |
| AddOrUpdateWiFiNetwork | the cluster stages the network (one) and its credential, nothing reaches the radio |
| ConnectNetwork | the driver hands the network to wpa_supplicant and waits for the interface's `completed` state, at most 20 s; on success the node re-announces its DNS-SD records at once |
| CommissioningComplete | the cluster commits the network into the node's store |
| `Node.Start` | the committed network is rejoined before the fabrics' records go out, which the start sequence then publishes |

The host's part:

- wpa_supplicant runs with its D-Bus interface (`wpa_supplicant -u`, or the
  distribution's `wpa_supplicant.service`), and the Wi-Fi interface is not
  managed by NetworkManager at the same time (`nmcli device set wlan0
  managed no`, or an `unmanaged-devices` entry);
- the process may talk to `fi.w1.wpa_supplicant1` on the system bus (root,
  or a D-Bus policy that allows its user or group);
- addressing is the host's network stack's: the driver runs no DHCP
  client. Matter needs the IPv6 link-local address the kernel configures
  on the interface; an IPv4 or global address is the host's affair;
- wpa_supplicant's configuration file stays untouched: the committed
  network lives in the node's store, so the credential is kept in one
  place, and the node rejoins it at start and whenever a join left it off
  that network.

The credential is never logged, nor part of a debug text or an error. An
SSID that is valid UTF-8 goes to wpa_supplicant as a string, any other as
its bytes; a credential of exactly 64 hex digits is the raw PSK, any other
a passphrase. Empty credentials join an open network. A join keeps the
network the interface was on until it has ended: one that does not
succeed removes the network it added, so wpa_supplicant does not keep
trying it, and selects the earlier network again; one that succeeds
removes the earlier network. A network that was joined stays configured
in wpa_supplicant; reconnecting it after a lost link is left to
wpa_supplicant. The rejoin at
`Node.Start` holds `Start` for up to the connect timeout when the network
is out of range; when it fails, the node tries again after 5 s, the wait
doubling up to 5 minutes, for its lifetime, on its clock
(`Config.Clock`). A fail-safe expiry that leaves the link on none of the
committed networks starts the same rejoin at once, and so does a
`CommissioningComplete`. An attempt waits while a fail-safe is armed or a
NetworkCommissioning command runs, and each one sets
`LastNetworkingStatus`. After a join that failed, the node asks a
driver that reports its link (`contract.WiFiLinkReporter`, which this
driver implements) where the radio is: `Networks` reads that network
connected — the one the failed join went back to — and a fail-safe expiry
or `CommissioningComplete` that finds it among the committed networks
joins nothing. A driver that does not report its link leaves no network
read as connected after a failed join. The driver resolves the
interface once, at `New`: after a wpa_supplicant restart the host builds a
new driver. When the system-bus connection itself is lost, every later scan
and join fails at once with UnknownError and the debug text "the driver's
bus connection is gone" — the same signal to build a new driver. A scan or
join also ends with its caller's context while it waits for the driver.

*Source: [`node/wifi_driver.go`](https://github.com/SukramJ/go-fabric/blob/main/node/wifi_driver.go) (`reannouncingDriver`),
[`cluster/core/network_commissioning_wifi.go`](https://github.com/SukramJ/go-fabric/blob/main/cluster/core/network_commissioning_wifi.go) (`Start`, `connect`),
[`wifi/wpasupplicant/driver.go`](https://github.com/SukramJ/go-fabric/blob/main/wifi/wpasupplicant/driver.go) (`New`, `ScanNetworks`, `ConnectNetwork`, `CurrentNetwork`).*

## mDNS

With `node.Config.Advertiser` unset the node runs its own DNS-SD responder:
it publishes `_matterc._udp` while a commissioning window is open and
`_matter._tcp` once a controller has paired, one record per fabric. A
second responder of the node's answers the discriminator subtype queries
(`_L<discriminator>._sub._matterc._udp`) that Apple Home, Google Home and
chip-tool send for a pairing code, and the address questions for the node's
own host name. It joins the mDNS multicast groups beside the main responder
on UDP 5353, which is how it shares the port.

The responder answers a record by multicast at most once a second on an
interface (RFC 6762 §6), as matter.js does: a query that asks again
within the second is answered when the second has passed, in one deferred
message, unless the querier already received the record. Services
published together — the commissionable and the operational record of a
starting node — share one announcement per interface. A multicast that an
interface refuses, or that reaches only one of its address families, logs
one `mdns.responder.multicast_not_sent` or
`mdns.responder.multicast_some_families` line at Info per outage; the
following misses log at Debug until a multicast reaches every family
again.

The responder answers a query only from a source on the link of the
interface the query arrived on: an IPv6 link-local address (`fe80::/10`),
or an address inside one of the prefixes of that interface's own
addresses. Its socket binds the wildcard address, so a unicast query from
anywhere reaches port 5353; one from off the link gets no answer, by
multicast or unicast, and is not held as a truncated query. matter.js drops
the same queries: it serves a packet on the interface whose address and
netmask contain the source, or the IPv6 zone the source carries, and
answers none it finds no interface for. The sources a real network has that
the check refuses:

- an IPv4 link-local querier (`169.254.0.0/16`, RFC 3927 — a host whose
  DHCP failed) while the node's interface holds no `169.254` address of its
  own; the IPv6 queries it sends from its `fe80::` address are answered;
- a querier of another IPv4 subnet or IPv6 prefix that shares the layer-2
  segment (overlayed subnets, a second prefix the node holds no address
  in), an IPv6 ULA among them;
- every other querier when the interface's only address of that family is
  a single host (`/32`, `/128`): its prefix contains no one else;
- a unicast query from across a router. A query an mDNS reflector re-sends
  from its own address on the link is answered.

A host that already runs avahi-daemon keeps it: this project's own
chip-tool harness runs the node on hosts with a running avahi-daemon. A
host that must not answer mDNS sets `Network.MDNS.Off`; no controller
then discovers the node, and a controller reaches it by address alone.
`Node.MDNSServices()` lists what the node advertises now, and
`Node.DiagnoseMDNS()` reports what would keep a controller from finding or
commissioning it.

The responder announces and answers on every multicast-capable interface,
each with that interface's own addresses, and leaves container and
virtualisation bridges out by name (`docker*`, `br-*`, `veth*`, `hassio*`,
… — `mdns.DefaultInterfaceFilter`). `Network.MDNS.Interfaces` restricts it
to the named interfaces: `[]string{"br30"}` announces and answers on `br30`
alone. The default filter still applies to a named interface, so naming
`docker0` or `hassio` does not publish its address. A name the host does
not have is accepted — interfaces come and go, and the interface check
picks it up when it appears — and `Node.DiagnoseMDNS()` reports it
(`interface_not_found`), as it does a named bridge (`interface_filtered`)
and a named interface without an advertised address
(`interface_no_addresses`). The list does not scope the UDP listen socket;
`Network.Listen` takes a host address for that.

A node may start with no multicast interface up at all — a node
commissioned over BLE onto Wi-Fi has none before it joins the network. The
responder's sockets stay open then, and every interface that comes up later
is joined to the mDNS groups at the next publication or interface check
(the node re-announces after a successful Wi-Fi join, which is the next
publication).

The case for it is a dual-homed host behind a gateway that relays mDNS
between its networks (an mDNS reflector or proxy): the node answers each
network with that network's address, the gateway relays the other
network's answer as well, and the controllers cache two addresses for one
host, one of them across the router. Advertising on the controllers'
network alone keeps the second address out. matter.js publishes on every
interface; the allow-list is this module's (*Ahead of matter.js*,
`BD-matterjs-mdns-interface-allowlist`).

*Source: [`mdns/doc.go`](https://github.com/SukramJ/go-fabric/blob/main/mdns/doc.go),
[`mdns/responder.go`](https://github.com/SukramJ/go-fabric/blob/main/mdns/responder.go) (`Responder`),
[`mdns/answer.go`](https://github.com/SukramJ/go-fabric/blob/main/mdns/answer.go) (`onLink`; `TestResponder_AnswersOnlyOnLinkSources`),
[`node/mdns.go`](https://github.com/SukramJ/go-fabric/blob/main/node/mdns.go) (`MDNS`, `MDNSServices`, `DiagnoseMDNS`),
[`mdns/interface_filter.go`](https://github.com/SukramJ/go-fabric/blob/main/mdns/interface_filter.go) (`DefaultInterfaceFilter`),
[`mdns/diagnose_interfaces.go`](https://github.com/SukramJ/go-fabric/blob/main/mdns/diagnose_interfaces.go) (`DiagnoseInterfaces`),
[`internal/chiptool/doc.go`](https://github.com/SukramJ/go-fabric/blob/main/internal/chiptool/doc.go) (the harness).*

## Logs

The node logs through `node.Config.Logger` — the daemon's own
`*slog.Logger`, so its handler and its level apply. The
[observability guide](observability.md) names every event prefix and
attribute key. The events that matter in operation:

| Event | When |
| --- | --- |
| `commissioning.own_window.open` (with `discriminator`) | the node opened its own commissioning window: at `Start` without a fabric, and after the last fabric is removed |
| `commissioning.window.open` (with `discriminator`) | a paired controller opened a window for another controller |
| `commissioning.window.closed` | the window closed: paired, timed out or revoked |
| `matter.opcreds.cmd_ok` | an OperationalCredentials command succeeded — AddNOC is the pairing landing, RemoveFabric a controller leaving |
| `commissioning.no_fabric_loaded` (error) | the store holds fabrics none of which loaded: the node does not open itself for commissioning rather than invite a stranger onto a bridge its owners still administer |
| `pase.*` | a pairing attempt's PASE handshake; repeated failures end in a lockout (below) |
| `matter.subscription.reestablished`, `matter.subscription.reestablish.*` | after a restart, the node re-establishing the subscriptions it persisted |

A host that wants the pairing events as calls rather than lines sets
`node.Config.Observers` (`OnFabricAdded`, `OnFabricRemoved`,
`OnFabricUpdated`). `node.Config.Metrics` counts sessions, subscriptions,
PASE failures and lockouts for a dashboard.

*Source: [`node/commissioning.go`](https://github.com/SukramJ/go-fabric/blob/main/node/commissioning.go),
[`cluster/core/operational_credentials.go`](https://github.com/SukramJ/go-fabric/blob/main/cluster/core/operational_credentials.go),
[`node/node.go`](https://github.com/SukramJ/go-fabric/blob/main/node/node.go),
[`node/observers.go`](https://github.com/SukramJ/go-fabric/blob/main/node/observers.go).*

## Health

`node.Config.Health` runs the node's health probe from `Start` to `Stop`:
every `Interval` (default 30 s) it checks that the operational UDP listener
is bound and hands the verdict to the callbacks `Node.OnHealthSample`
(inline) and `Node.OnHealthSampleAsync` (queued) registered;
`Node.Health()` returns the last one. A liveness probe of the daemon asks `Node.Health()` and treats
`Healthy == false`, or no sample at all with the probe configured, as
failing. The zero value runs no probe.

*Source: [`node/health.go`](https://github.com/SukramJ/go-fabric/blob/main/node/health.go).*

## Limits

Every table a peer can fill has a fixed size; the [limits page](limits.md)
lists them all. The three an operator meets first:

- **Fabrics: 5** (`node.Config.SupportedFabrics`, up to 254). Each
  controller ecosystem the node is paired with is one fabric; a sixth
  pairing is answered `TableFull`.
- **PASE failures per commissioning window: 20**, then a lockout of
  15 minutes, doubling up to 4 hours: a pairing code typed wrong twenty
  times closes the window for a while.
- **Subscriptions per fabric: 16**; a controller asking for more is
  answered `ResourceExhausted`.

## A failed pairing

When a controller says the pairing failed, the node's diagnostic trace
holds the moments that tell why: a handshake that completed or did not, a
session that closed, a lockout that engaged, the node re-announcing
itself. `Node.DiagnosticEvents()` returns it, newest first;
`Node.OnDiagnosticEvent` hands each event as it is recorded. The ring keeps
256 events by default (`node.Config.Diagnostics`) and nothing of it is
persisted — the logs are the durable record. Together with
`Node.DiagnoseMDNS()` (was the node findable?) and the `pase.*` lines (did
the code match?), it separates the three usual causes: the controller never
found the node, the code was wrong, or the commissioning steps after PASE
failed.

*Source: [`node/diagnostics.go`](https://github.com/SukramJ/go-fabric/blob/main/node/diagnostics.go), the
[observability guide](observability.md#the-diagnostic-trace).*

## Factory reset

The node has no reset call of its own. What a reset is:

- **Removing the fabrics** — `Node.RemoveFabric(ctx, index)` for each
  fabric, as a controller's RemoveFabric command does: the fabric's rows,
  ACL and keys go, its sessions and subscriptions close, and after the last
  one the node opens its own commissioning window again. The store keeps
  the rest: the endpoint numbers, labels, the UniqueID. The node stays the
  same node, with nobody paired.
- **Removing the file** — stopping the node and deleting the store file
  (and its `-journal`) starts the next boot from nothing: no fabric, and
  new endpoint numbers for the bridged devices. The UniqueID is derived
  again from the vendor, product, NodeLabel and serial number, so it comes
  back the same unless one of them changed. Controllers that were paired
  keep a dead entry until they are told to forget it.

The host's own state — its configuration, its devices — is untouched by
either.

*Source: [`node/node.go`](https://github.com/SukramJ/go-fabric/blob/main/node/node.go) (`RemoveFabric`), the
[persistence guide](persistence.md#what-survives-a-restart).*

## Upgrading

A new go-fabric release can move three things a running daemon notices:

- **The data model.** Each release embeds the matter.js snapshot it was
  generated from; `schema.SchemaSnapshotSHA256` is its digest. A changed
  digest means cluster revisions, attributes or device-type requirements
  may have moved — what a controller reads from the node after the
  upgrade. A daemon that wants to see that coming pins the digest in a test
  of its own and updates the pin with the dependency bump.
- **The store.** The node applies its schema on every boot (`Apply` is
  idempotent; an upgrade step runs once); the
  [persistence guide](persistence.md#schema-changes) describes the versioned
  migrations and the fingerprint for a host that runs its own tool. Back
  the file up before an upgrade.
- **The API.** [`CHANGELOG.md`](https://github.com/SukramJ/go-fabric/blob/main/CHANGELOG.md)
  is the single place a break is announced: every `**Break:**` line names
  what moved and its replacement, and the
  [migration guide](migrating-to-the-facade.md) has a section per release
  that changed the facade. Read both before a bump.

*Source: [`schema/schema_provenance_gen.go`](https://github.com/SukramJ/go-fabric/blob/main/schema/schema_provenance_gen.go),
[`README.md`](https://github.com/SukramJ/go-fabric/blob/main/README.md) (API stability).*
