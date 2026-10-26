# reference-controller

The module's controller role as a command-line tool: it pairs Matter nodes
over IP or BLE, reads, writes, invokes and subscribes, provisions groups and
bindings, and serves an OTA image. It is an example (the command line is
`package cli` beside this file), not API — copy from it, do not build
against it.

```sh
go build ./examples/reference-controller
./reference-controller --db home.db pair --code MT:-24J0AFN00KA0648G00
```

This build has no BLE. The BLE build is
[`ble/bluez/cmd/reference-controller`](../../ble/bluez/cmd/reference-controller/main.go),
the same command line over BlueZ (§BLE below).

Every command opens the database, does its work and exits; the next command
is the same fabric, so every node paired before still trusts it.

## Global flags

They go before or after the command.

| Flag | Default | |
| --- | --- | --- |
| `--db <file>` | `reference-controller.db` | The SQLite database (see [The database](#the-database)). |
| `--trust development\|production\|none` | `development` | Which roots `pair`'s attestation check trusts: the embedded DCL snapshot (`controller/dcl/snapshot`) and the CSA test PAAs, the snapshot only, or none (no chain check). The snapshot's fetch time is in the `--log-level debug` log. |
| `--trust-dir <path>` | unset | A DCL snapshot directory (`paa/`, `cd-signers/`, `SOURCE`, as `make update-dcl-snapshot` writes it) in place of the embedded one — a newer set without a rebuild. |
| `--dcl prod\|testnet\|off` | `off` | The online CSA Distributed Compliance Ledger (or its test net) on top of the trust set: roots newer than the snapshot, CD signers fetched on demand, and the CRL revocation check, which only the online ledger answers. |
| `--attestation accept\|reject\|ask` | `ask` on a terminal, `reject` otherwise | What an attestation finding does to a pairing. |
| `--log-level debug\|info\|warn\|error` | `warn` | The log, as `slog` text on stderr. |
| `--listen <addr>` | `[::]:0` | The controller's UDP socket. |
| `--node-listen <addr>` | `[::]:0` | The UDP socket of the controller's own node (group sender, OTA provider). |
| `--timeout <duration>` | `60s` | How long a command waits for the network (a bare number is seconds). |
| `--ca-key file:<path>` | unset | The CA root key in a passphrase-encrypted key file instead of the database; the passphrase comes from `GOFABRIC_KEY_PASSPHRASE`. A missing file is created: with the root key the database holds (which then leaves the database), or a new key for a new database. Every later run needs the flag. |

A `<node>` argument is the node id, decimal or `0x` hex, or the registry's
local id (`peer1`). A path is `<endpoint>/<Cluster>/<element>`: the cluster
by its matter.js name (`OnOff`, `BasicInformation`) or id, the element — an
attribute, command or event — by name or id; `*` is a wildcard where the
Interaction Model allows one. A JSON value follows chip-tool's conventions
(`cluster/spec/json.go`): numbers, or `"0x…"` strings for unsigned ones;
octet strings as `"hex:…"`, `"base64:…"` or `"str:…"`; structs as objects by
field name; lists as arrays; `null`; an enum also by value name, a bitmap
also as an object of member names. A command fails with exit status 1 and
its error on stderr.

## Commands

- `pair --code <MT:… | 11 or 21 digits> | --passcode N --discriminator N [--node-id N] [--label L] [--wifi-ssid S --wifi-credentials C | --thread-dataset HEX] [--ble [--ble-adapter hci0]]`
  Finds the device over DNS-SD by its discriminator (long from a QR code or
  `--discriminator`, short from a manual code), runs PASE, checks its
  attestation, installs the NOC, keeps the node in the registry and prints
  its node id and device view (each endpoint's device type and clusters).
  `--label` writes `BasicInformation.NodeLabel`. `--wifi-ssid` with
  `--wifi-credentials`, or `--thread-dataset` (the operational dataset as
  hex), add the network steps of the commissioning flow
  (`AddOrUpdateWiFiNetwork` / `AddOrUpdateThreadNetwork`, `ConnectNetwork`),
  which matter.js runs only on a BLE channel; here they also run over IP
  when the credentials are given (`BD-Controller-NetworkStepsOverIP`).

  ```sh
  reference-controller pair --code 34970112332 --node-id 0x4321 --label kitchen
  ```

  `--ble` (the BLE build only) finds the device by its BLE advertisement
  instead — by the same discriminator — and runs PASE over BTP on the
  adapter `--ble-adapter` (default `hci0`). A device on BLE has no network
  yet, so `--ble` needs `--wifi-ssid` with `--wifi-credentials`, or
  `--thread-dataset`; the network steps put the device on that network,
  and operational discovery then finds it over DNS-SD for CASE. In the plain
  build `--ble` answers that the build has none.

  ```sh
  reference-controller pair --ble --code MT:-24J0AFN00KA0648G00 --wifi-ssid lab --wifi-credentials secret12
  ```

- `discover [--code <MT:… | 11 or 21 digits> | --discriminator N] [--first]`
  — searches DNS-SD for the commissionable devices the code's or the long
  discriminator selects (every one with neither) until `--timeout` passes,
  and prints each with its discriminator, vendor and product, commissioning
  mode and addresses; `--first` stops at the first that answers
  (`Controller.DiscoverWith`, `DiscoverOptions.First`).
- `unpair <node>` — RemoveFabric on the node, then its record is deleted.

  ```sh
  reference-controller unpair 0x4321
  ```

- `nodes` — every paired node: node id, local id, label, whether it answers
  a read within `--timeout` (5 s here unless given), and the conformance
  verdict of its image.

  ```sh
  reference-controller nodes
  ```

- `node <node>` — reads every attribute and prints the node's full image:
  endpoints, device types, conformance, clusters, attribute values.

  ```sh
  reference-controller node 0x4321
  ```

- `read <node> <ep>/<Cluster>/<Attribute>…` — one JSON line per attribute.

  ```sh
  reference-controller read 0x4321 0/BasicInformation/NodeLabel '*/OnOff/OnOff'
  ```

- `write <node> <ep>/<Cluster>/<Attribute> <json> [--timed ms]`

  ```sh
  reference-controller write 0x4321 0/BasicInformation/NodeLabel '"pantry"'
  ```

- `invoke <node> <ep>/<Cluster>/<Command> [<json>] [--timed ms]` — prints the
  status and the response's fields as a JSON line.

  ```sh
  reference-controller invoke 0x4321 1/LevelControl/MoveToLevel '{"level": 128, "transitionTime": 10, "optionsMask": 0, "optionsOverride": 0}'
  ```

- `subscribe <node> <path>… [--min s] [--max s] [--count n]` — attribute or
  event paths; every report as JSON lines until Ctrl-C (or `--count`
  reports).

  ```sh
  reference-controller subscribe 0x4321 1/OnOff/OnOff --max 30
  ```

- `events <node> <ep>/<Cluster>/<Event>…` — reads events, one JSON line each.

  ```sh
  reference-controller events 0x4321 0/BasicInformation/StartUp
  ```

- `group create <id> --key <hex|random> --nodes <n,n…> --endpoints <e,e…> [--keyset N]`
  — writes the key set (GroupKeyManagement KeySetWrite) on each node and
  joins the group on its endpoints with Groupcast JoinGroup and the
  auxiliary ACL entry; the controller's own node gets the key set as a
  sender-only member. The key set id defaults to the group id. A node whose
  root ServerList lacks Groupcast (before Matter 1.5) is provisioned the
  pre-1.5 way instead: GroupKeyMap entry, Groups AddGroup on each endpoint
  and a Group ACL entry granting the group Operate on them.

  ```sh
  reference-controller group create 257 --key random --nodes 0x4321,0x4322 --endpoints 1
  ```

- `group send <id> <Cluster>/<Command> [<json>]` — a group command from the
  controller's own node.

  ```sh
  reference-controller group send 257 OnOff/Toggle
  ```

- `bind <node> <ep> --to <node>/<ep>[/<Cluster>]` — adds the Binding entry on
  the source endpoint and the ACL entry on the target that grants the source
  Operate on the target endpoint (and cluster).
  `bind <node> <ep> --group <id>` — adds a group Binding entry and gives the
  source the group's key set and GroupKeyMap entry.

  ```sh
  reference-controller bind 0x4322 1 --to 0x4321/1/OnOff
  ```

- `ota serve --image <file> [--version N] [--vendor N] [--product N] [--announce <node>] [--once] [--yes]`
  — serves the image through the controller's OTA push
  (`controller.Config.OTA`): the image goes into the update manager's store
  and is offered by its header, so `--version`, `--vendor` and `--product`
  must match it. With `--announce` the console grants that node's consent
  (unless `--yes`) and the manager announces the provider and follows the
  update — the node gets a default subscription for the time it runs;
  without it every paired node of the vendor and product gets the consent
  and the image when it queries. Each status is printed. Runs until Ctrl-C,
  or `--once` until one update is done.

  ```sh
  reference-controller ota serve --image light-v2.ota --announce 0x4321 --once
  ```

- `window open <node> [--timeout s] [--basic]` — opens a commissioning
  window on a paired node so a second administrator (Apple Home, Google
  Home, another controller) can add it. Enhanced (the default) draws a
  random discriminator, passcode and 32-byte salt, sends
  OpenCommissioningWindow with the PAKE verifier (1000 PBKDF iterations)
  and prints the discriminator, passcode, manual code and QR code; the
  node's vendor and product ids are read for the QR code. `--basic` sends
  OpenBasicCommissioningWindow — the device's own code — and fails when
  the node does not advertise the Basic (BC) feature. `--timeout` is the
  window's lifetime in seconds (default 900); after `window open` it is the
  window's, so the network `--timeout` goes before the command. Any window
  already open is revoked first. Both follow matter.js's
  `CommissioningClient` (`openEnhancedCommissioningWindow`,
  `openBasicCommissioningWindow`).
  `window revoke <node>` — RevokeCommissioning; a node without an open
  window is reported, not an error.

  ```sh
  reference-controller window open 0x4321 --timeout 600
  ```

## BLE

```sh
cd ble/bluez && go build -o reference-controller ./cmd/reference-controller
```

`ble/bluez` is a Go module of its own (ADR 0024 decision 3), so the main
module never pulls its D-Bus dependency; its `cmd/reference-controller` is
this command line with `cli.Options.Central` set to `bluez.NewCentral`. It
speaks BlueZ over the D-Bus system bus, without cgo, on Linux only.

- **Rights.** BlueZ answers callers its D-Bus policy admits: root and, on
  most distributions, members of the `bluetooth` group
  (`sudo usermod -aG bluetooth $USER`, then log in again); elsewhere a
  polkit rule or a D-Bus policy file for `org.bluez`. Without the right
  `pair --ble` fails with `org.freedesktop.DBus.Error.AccessDenied` in the
  message; an adapter that is off fails with `org.bluez.Error.NotReady`
  (`bluetoothctl power on`).
- **BlueZ.** Verified against BlueZ 5.72 over virtual controllers only:
  the module's tests run over an in-process bus, and the live path is the
  manually started `ble-virtual` workflow (`make ble-virtual-test`; Ubuntu 24.04 on arm64,
  two LE-only `btvirt` controllers): this build pairs a matter.js node
  with `@matter/nodejs-ble` (`script/ble-virtual/peripheral.mjs`, bleno,
  a Wi-Fi NetworkCommissioning server) with `pair --ble`, reads it over
  CASE and opens a window, and matter.js's own controller pairs the same
  peripheral as the control leg. It is a virtual counterpart, not a field
  test: a real device stays the proof.
  The ATT_MTU comes from the characteristic's `MTU` property; a BlueZ
  without it runs BTP at the 20-byte minimum segment.
- **Adapter.** `--ble-adapter` names the adapter as BlueZ does (`hci0`,
  `hci1`, …; `bluetoothctl list`).

## The controller's own node

A group message is sealed and sent by a node of the fabric, and an OTA
image is served by one; the controller role has neither. `group` and `ota`
therefore open the controller with its OTA push, whose own node
(`Controller.OwnNode`, started by `node/controllernode`) the controller
commissions into the tool's fabric on first use and keeps in the same
database. It serves no device; it advertises on DNS-SD like any node while it
runs. `pair`, `read`, `write`, `invoke`, `subscribe`, `events`, `bind` and
`unpair` do not start it.

## The database

One SQLite file holds:

| Where | What |
| --- | --- |
| `matter_settings` `controller.ca` | The CA: root certificate, root key, certificate counter (JSON, matter.js `CertificateAuthority` field names). With `--ca-key` the root key is not here; `rootKeyRef` names the key file. |
| `matter_settings` `controller.ca.fabric` | The fabric: fabric id, root node id, vendor id, IPK, fabric index, label, the controller's operational key and NOC (matter.js `Fabric.Config` names). |
| `matter_settings` `controller.dcl.trust` | The DCL trust store, with `--dcl prod` or `testnet`. |
| `matter_settings` `controller.ownnode`, `controller.group.<id>` | The own node's passcode, discriminator, salt and node id (moved from `controller.self` on the first start of v0.13.0); each group `group create` made (key set, epoch key, members). |
| `matter_client_values`, `matter_client_versions` | The client registry: every paired node's record and cached image (`controller/client/sqlstore`). |
| the other `matter_*` tables | The own node's store (its fabric, keys, ACL). |

The file holds private keys: keep it as private as the devices it controls.
CASE resumption records are kept in memory only, so the first command of a
run makes a full CASE handshake.

## What leaves the host

With `--dcl off` (the default) nothing but Matter traffic: DNS-SD on the local
network and the Matter messages to the devices. The DCL snapshot is embedded
in the binary; using it asks nobody. Without `--dcl` no revocation is
checked: the snapshot holds trust anchors, not CRLs.

With `--dcl prod` or `--dcl testnet`, `pair` — and only `pair` — opens the
ledger trust store (`controller/dcl`): it updates the store from the ledger
(`https://on.dcl.csa-iot.org`, or `https://on.test-net.dcl.csa-iot.org`)
over HTTPS — the PAA list — and, while it attests the device, looks up the
CD signer and the revocation points by their subject key identifiers. The
ledger learns which manufacturers' devices the controller commissions; no
device identity, passcode or fabric key is sent. The store is kept in the
database between runs. GitHub's development certificates are not fetched.
The other commands, and the own node, never contact the ledger.
