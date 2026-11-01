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
| TCP on the same port | off | Only with `Network.TCP.Incoming`: the node then also accepts Matter over TCP and says so in its session parameters, and the TCP listener takes the UDP socket's port. |
| IPv6 on the LAN | required | Matter's operational network is IPv6: the node advertises its IPv6 addresses and controllers reach it there. `Network.PreferIPv4` forces an IPv4-only socket, for a test network. |
| UDP 5353, multicast `224.0.0.251` and `ff02::fb` | required | DNS-SD over mDNS, below. |

A firewall in front of the node lets in UDP 5540 (and TCP 5540 with TCP on)
and mDNS from the controllers' LAN. A container sees the LAN's multicast
only with host networking or an equivalent.

*Source: [`node/config.go`](https://github.com/SukramJ/go-fabric/blob/main/node/config.go) (`Network`),
[`bridge/tcp_listen.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/tcp_listen.go) (`TCPConfig`, `openTransports`).*

## mDNS

With `node.Config.Advertiser` unset the node runs its own DNS-SD responder:
it publishes `_matterc._udp` while a commissioning window is open and
`_matter._tcp` once a controller has paired, one record per fabric. A
second responder of the node's answers the discriminator subtype queries
(`_L<discriminator>._sub._matterc._udp`) that Apple Home, Google Home and
chip-tool send for a pairing code, and the address questions for the node's
own host name. It joins the mDNS multicast groups beside the main responder
on UDP 5353, which is how it shares the port.

A host that already runs avahi-daemon keeps it: this project's own
chip-tool harness runs the node on hosts with a running avahi-daemon. A
host that must not answer mDNS sets `Network.MDNS.Disabled`; no controller
then discovers the node, and a controller reaches it by address alone.
`Node.MDNSServices()` lists what the node advertises now, and
`Node.DiagnoseMDNS()` reports what would keep a controller from finding or
commissioning it.

*Source: [`mdns/doc.go`](https://github.com/SukramJ/go-fabric/blob/main/mdns/doc.go),
[`mdns/subtype_responder.go`](https://github.com/SukramJ/go-fabric/blob/main/mdns/subtype_responder.go) (`SubtypeResponder`),
[`node/mdns.go`](https://github.com/SukramJ/go-fabric/blob/main/node/mdns.go) (`MDNS`, `MDNSServices`, `DiagnoseMDNS`),
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
is bound and hands the verdict to `OnSample`; `Node.Health()` returns the
last one. A liveness probe of the daemon asks `Node.Health()` and treats
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
