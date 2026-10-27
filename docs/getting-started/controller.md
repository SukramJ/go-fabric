# Getting started: the controller side

The controller role pairs Matter nodes onto a fabric this module owns. It
then reads, writes, invokes and subscribes on those nodes. It entered the
module in stages under [ADR 0020](../adr/0020-commissioner-and-controller-role.md)
and lives under `controller/`. This page builds the stack, pairs a node and
uses it. The program is `examples/quickstart-controller/main.go`. The
complete tool, with every command, is `examples/reference-controller`.

## The stack

`controller.New(ctx, controller.Config{…})` builds the controller in one
call. It is the role's composition root (ADR 0026 principle 2): it
applies the database schemas and assembles the nine parts in the order
the reference controller's `openStack` builds them:

| Step | Package | What it is |
| --- | --- | --- |
| Database | `store`, `controller/client/sqlstore` | the settings table and the client images; both schemas applied |
| Certificate authority | `controller/ca` | the fabric's root of trust; issues our NOC and every node's |
| Fabric | `controller/commissioner` (`NewFabric`) | our operational identity on the fabric |
| Socket | `transport/udp`, `client/exchange` | one UDP socket; sessions and exchanges with MRP |
| DNS-SD | `client/discovery` | the browser for commissionable and operational records |
| Peers | `client/peer` | the nodes of our fabrics, with CASE on demand |
| Subscriptions | `client/interaction` | the registry of client subscriptions |
| Commissioner | `controller/commissioner` (`New`) | PASE, attestation, CSR, NOC, CommissioningComplete |
| Registry | `controller/client` | one image per remote node, persisted |

`Controller.Close` tears the stack down in reverse; the database stays
the host's. `Config` takes:

- `DB` (required): `controller.OpenDB(path)` opens a SQLite file with the
  module's own pure-Go driver (foreign keys on, a 5 s busy timeout, the
  write-ahead log), so the program imports no driver; a host may open the
  database itself with the driver of its choice.
- `Attestation` (required): the PAA trust store. There is no default, so
  a controller never trusts the test PAAs by accident.
  `attestation.TestStore()` is for development devices;
  `controller.ProductionTrust()` is the production set of the DCL snapshot
  embedded in the module, without the test roots and without a network;
  a controller that also asks the online ledger layers a
  `dcl.CertificateStore` over it with `attestation.Trust`.
- `Listen`, `VendorID` (0 is the test vendor 0xFFF1), `Label`, `Logger`,
  `Node` (every client node's `client.Config`).
- `ManualStart`: false starts the commissioned nodes, as matter.js's
  controller connects them on start. A program that only pairs or reads
  once sets it.
- `Clock` and `Listener`: the stack's time source (nil is the system
  clock) and a UDP socket to serve on in place of opening `Listen`. A test
  passes both: `fabrictest.Site.ControllerConfig(opts)` returns a `Config`
  wired to the site — its network, its DNS-SD answers, its fake clock and
  a database of its own — so the program's own `controller.New` runs on
  the site (see [testing in process](../guides/testing-in-process.md)).
- `CAKey`: the certificate authority's root key held outside the
  database, a P-256 `crypto.Signer` (a key file through
  `controller/ca/keyfile`, an HSM, a KMS). nil keeps the root key in the
  database; with it, a stored root private key leaves the database.

`Controller.Registry`, `Controller.Commissioner`, `Controller.Fabric` and
`Controller.Peers` are the role's seams
([advanced seams](../guides/advanced-seams.md)). The reference controller
builds on `controller.New`: it composes its trust set
(`attestation.Trust` over the DCL snapshot and the online ledger) into
`Attestation`, opens its CA key file into `CAKey` and its socket into
`Listener`, and pairs through `Controller.Pair`, over BLE with
`PairOptions.BLE`; its own node is reached through `Peers` and
`Commissioner().Interaction`.

*Source: `controller/controller.go`, `controller/open.go`, `fabrictest/controller_config.go`, `examples/reference-controller/cli/stack.go:114-190`, `examples/reference-controller/cli/pair.go` (`cmdPair`), `examples/quickstart-controller/main.go`.*

## The program

<!-- program: examples/quickstart-controller/main.go -->
```go
// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command quickstart-controller is the smallest Matter controller on
// go-fabric: it keeps one fabric in a SQLite file, pairs a node by its QR or
// manual code, reads its NodeLabel, toggles its endpoint 1 OnOff and prints
// the OnOff reports of a subscription until Ctrl-C.
// docs/getting-started/controller.md walks through it; the reference
// controller (examples/reference-controller) is the complete tool.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
)

// openNames opens the DNS-SD cache; a test replaces it.
var openNames = discovery.Open

func main() {
	dbPath := flag.String("db", "quickstart-controller.db", "the SQLite file holding the fabric and the paired nodes")
	code := flag.String("code", "", "pair the node with this QR (MT:…) or manual code first")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, *dbPath, *code)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dbPath, code string) error {
	db, err := openDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	c, err := open(ctx, db, "[::]:0", slog.Default())
	if err != nil {
		return err
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
	if code != "" {
		noSubscribe := false // this program exits; a resident host keeps the default subscription
		if _, err := c.Pair(ctx, code, controller.PairOptions{AutoSubscribe: &noSubscribe}); err != nil {
			return err
		}
	}
	nodes := c.Nodes()
	if len(nodes) == 0 {
		return errors.New("no paired node: pass -code")
	}
	return use(ctx, nodes[0])
}

// openDB opens the SQLite file the controller keeps its fabric in, with
// the module's own driver.
func openDB(path string) (*sql.DB, error) { return controller.OpenDB(path) }

// open builds the controller over the database: the fabric (created on the
// first run, loaded afterwards), the socket, DNS-SD, peers, commissioner,
// registry — one call. The trust store is the CSA's test PAAs, for
// development devices; a production controller passes a DCL-backed one.
func open(ctx context.Context, db *sql.DB, listen string, log *slog.Logger) (*controller.Controller, error) {
	return controller.New(ctx, controller.Config{
		DB:          db,
		Attestation: attestation.TestStore(),
		Listen:      listen,
		Label:       "quickstart",
		Logger:      log,
		Names:       openNames,
		ManualStart: true, // the program reads and subscribes itself
	})
}

// use reads, invokes and subscribes through the node's client image.
func use(ctx context.Context, n *client.Node) error {
	var read client.ReadRequest
	read.Attributes = []client.AttributePath{client.ClusterAt(0, basicinfo.ClusterID)}
	if _, err := n.Read(ctx, read); err != nil {
		return err
	}
	// The typed read: the image's values as the generated State.
	if info, _ := basicinfo.StateFrom(n.Endpoint(0).Cluster(basicinfo.ClusterID)); info.NodeLabel != "" {
		fmt.Printf("NodeLabel: %s\n", info.NodeLabel)
	}
	toggle := client.CommandAt(1, onoffdef.ClusterID, onoffdef.CmdToggle)
	if _, err := n.Invoke(ctx, toggle, onoffdef.ToggleRequest{}, client.InvokeOptions{}); err != nil {
		return err
	}
	sub, err := n.Subscribe(ctx, client.SubscribeRequest{SubscribeRequest: client.SubscribePaths{
		Attributes:         []client.AttributePath{client.AttributeAt(1, onoffdef.ClusterID, onoffdef.AttrOnOff)},
		MaxIntervalCeiling: client.Interval(time.Minute),
		Updated: func(r *client.Report) { // keep it short: it runs on the report's exchange
			if _, ok := r.Value(1, onoffdef.ClusterID, onoffdef.AttrOnOff); ok {
				st, _ := onoffdef.StateFrom(r.Values(1, onoffdef.ClusterID))
				fmt.Printf("OnOff: %v\n", st.OnOff)
			}
		},
	}})
	if err != nil {
		return err
	}
	defer sub.Close()
	select {
	case <-ctx.Done():
	case <-sub.Done():
	}
	return nil
}
```

```sh
go run ./examples/quickstart-controller -code MT:-24J0AFN00KA0648G00
go run ./examples/quickstart-controller          # the same node, next run
```

The test opens the controller twice on one database and checks that the
second run is the same fabric. It also checks that a malformed code is
refused and that a valid code that no device answers ends at the deadline
with `controller.ErrNoDevice`.

*Source: `examples/quickstart-controller/main_test.go`.*

## The fabric

`ca.New` with a store keeps the root key and the certificate counter. The
CA saves after every certificate it issues, so serial numbers do not repeat
after a restart. `commissioner.NewFabric` with a store loads the stored
fabric record (fabric id, our node id, IPK, index, label, key and NOC). It
mints and saves a new record only when the store has none.

A stored record wins over the configuration. A non-zero `FabricID` or
`RootNodeID` that differs from the stored record is `ErrFabricMismatch`.
That is why `controller.New` sets both fields to zero once a record exists.
`ca.SettingStore` keeps the CA under the settings key `controller.ca` and
the fabric under `controller.ca.fabric`.

The database therefore holds the fabric's private keys. Anyone with the
file controls every node paired to it.

*Source: `controller/ca/doc.go:4-17`, `controller/commissioner/fabric.go:23-69`, `controller/ca/persist.go:94-115`, `examples/reference-controller/README.md` (The database).*

## Pairing

`Controller.Pair(ctx, code, controller.PairOptions{})` runs the four steps
below. Its `OnAttestationFailure` defaults to `attestation.Reject()`, and
`AutoSubscribe` overrides the registry's default subscription. A context
without a deadline is bounded by `controller.DefaultPairTimeout` (one
minute).

1. **Decode the code.** `setup.DecodeQRCode` reads an `MT:` payload and
   gives the long discriminator. `setup.DecodeManualCode` reads 11 or 21
   digits and gives the short discriminator. Both give the passcode.
2. **Find the device.** `discovery.NewScanner(names).FindCommissionable`
   calls back for each device that matches the discriminator.
3. **Make a node.** `Registry.ForDevice` returns the registry's node for
   the discovered device, creating one when needed.
4. **Commission.** `Registry.Commission` runs PASE to the device's
   addresses, then the commissioning flow: ArmFailSafe, device
   attestation, CSR, NOC, the operational CASE connection, and
   CommissioningComplete. The record and settings are saved, and the node
   starts.

`CommissionOptions` decides the rest:

- `NodeID`: 0 draws a random operational node id that no peer of the
  fabric has.
- `AutoSubscribe`: nil or true keeps the node live through one sustained
  wildcard subscription. The quickstart turns it off because it exits.
  It is the default subscription, narrowed by `DefaultSubscription`
  (`client.SubscribeOptions`); `Node.Subscribe` is a custom one beside it
  — the default for a host that wants the node's whole image kept,
  `Node.Subscribe` for paths of its own with its own `Updated`, or both.
  Both together need `DefaultSubscription.KeepSubscriptions`: without it
  the default subscription asks the node to drop the controller's other
  subscriptions each time it is (re-)established, and a custom one
  established with `ReplaceSubscriptions` drops the default.
- `NoInitialRead`: skips the initial read after commissioning.

A second `Commission` of a commissioned node is `ErrAlreadyCommissioned`.
A commission or decommission already running for that node is
`ErrFabricOperationInProgress`. `Registry.Decommission` sends RemoveFabric
to the node and then deletes it. If the node refused the removal, or the
removal was not confirmed, `Decommission` returns that error and keeps the
node.

A Wi-Fi or Thread device out of the box is found over BLE instead
([ADR 0024](../adr/0024-ble-commissioning-in-the-controller.md)):
`ble.NewScanner(central).Find` matches its advertisement by the same
discriminator, `ble.Dial` opens the BTP channel, and `Registry.Commission`
with `Options.Channel` runs PASE over it, then the network steps with
`Options.WiFiNetwork` or `ThreadNetwork`; operational discovery finds the
device over DNS-SD for CASE. The central on Linux is
`github.com/SukramJ/go-fabric/ble/bluez`, a module of its own so the main
module never pulls its D-Bus dependency; the reference controller's BLE
build is its `cmd/reference-controller` (`pair --ble`, rights and BlueZ
version in the reference controller's README).

*Source: `examples/reference-controller/cli/pair.go:37-189`, `examples/reference-controller/cli/pair.go:214-246`, `ble/bluez/doc.go:4-30`, `controller/client/fabricops.go:108-157`, `controller/client/fabricops.go:224-232`, `controller/commissioner/commissioner.go:85-123`, `controller/commissioner/doc.go:4-19`, `controller/client/registry.go:177-192`.*

## Attestation and the DCL

The commissioner checks the device's attestation against a trust store:

- **`attestation.TestStore()`** (the quickstart's choice) holds the CSA
  test PAAs and the test CD signer. It accepts the test devices that the
  reference bridge and chip-tool present.
- **A nil store** checks without one, as matter.js does without its DCL
  service.
- **`dcl.OpenCertificateStore`** is the opt-in trust store from the CSA
  Distributed Compliance Ledger. Nothing in `controller/dcl` reaches the
  network unless you construct a `dcl.Client` and hand it over. The ledger
  then learns the key identifiers of the PAAs, PAIs and CD signers of the
  devices you attest; no device identity, passcode or fabric key is sent.

The snapshot also names the vendors offline: `snapshot.Vendors()` and
`snapshot.Vendor(id)` are the ledger's vendor records it was built with,
over matter.js's static vendors (the test vendors 0xFFF1–0xFFF4 and Nabu
Casa), so a vendor id resolves to a name without the network.
`make update-dcl-snapshot` refreshes them with the certificates (or alone,
`go run ./script/dclsnapshot -vendors-only`).

The policy decides what a finding does:

| Policy | Effect on a finding |
| --- | --- |
| `attestation.Accept()` | proceeds; each finding is logged |
| `attestation.Reject()` | rejects the commissioning |
| `Policy{Decide: …}` | your function decides alone |
| zero value `Policy{}` | **proceeds**, with a warning per finding |

The zero value proceeds because matter.js does the same for backward
compatibility. Set the policy explicitly. (`controller.Pair` sets
`attestation.Reject()` when its options leave the policy unset.)

A rejection matches `commissioner.ErrAttestationRejected` through
`errors.Is`, besides `commissioner.ErrCommissioning`, so a host tells a
refused device from a network failure.

*Source: `controller/attestation/doc.go:4-20`, `controller/attestation/store.go:87-93`, `controller/attestation/policy.go:17-61`, `controller/commissioner/commissioner.go:52-54`, `controller/dcl/doc.go:4-24`, `examples/reference-controller/cli/stack.go:192-274`.*

## The registry and the device view

The `client.Registry` restores every node its store holds when it is built.
`Registry.Start` starts the commissioned, enabled nodes only when
`RegistryConfig.AutoStart` is set. Use these calls to find a node:

| Call | Returns |
| --- | --- |
| `Get(id)` | the node with a local id (`peer1`, `peer2`, …) |
| `GetByAddress(fabric.AddressOf(nodeID))` | the node with a node id |
| `Commissioned()` | every commissioned node |

A `client.Node` is the controller's image of the remote node. The
controller has no device model of its own: the remote node's Descriptor
decides the endpoints and the generated cluster definitions decide each
cluster's shape.

| Call | Returns |
| --- | --- |
| `Endpoints()`, `Endpoint(n)` | the image's endpoints |
| `Endpoint.DeviceType()`, `Endpoint.Device()` | the device type id, and its definition when known |
| `Endpoint.Clusters()`, `Endpoint.Cluster(id)` | the clusters |
| `<cluster>def.StateFrom(Endpoint.Cluster(id))` | the cluster's typed `State`, and the attributes the image lacks |
| `Cluster.Decoded(attr)` | one attribute value, untyped: a struct as its generated type, any other value at its widest Go type (a uint8 as `uint64`) |
| `Cluster.Value(attr)` | the raw TLV tree |

The typed read is the generated `StateFrom` of each `<cluster>def`
package. It fills the same `State` the device side's typed server reads,
through the same generated code, so a value has its attribute's own type.
It takes any `spec.Values`: a cluster of the image, or one cluster of a
report (`Report.Values(endpoint, cluster)`).

The image is persisted with `controller/client/sqlstore`, so a later run
starts from what the last one saw.

*Source: `controller/client/doc.go:19-50`, `controller/client/registry.go:24-192`, `controller/client/registry.go:345-371`, `controller/client/endpoint.go:56-148`, `controller/client/cluster.go:207-348`, `examples/reference-controller/cli/pair.go:191-212`.*

## Read, write, invoke, subscribe

All four go through the node and its CASE session. The peer set
establishes CASE when no session is live. Their types are
`controller/client`'s own: `AttributePath`, `CommandPath`, `EventPath`
(built with `client.AttributeAt`, `client.ClusterAt`, `client.CommandAt`,
`client.EventAt`), `ReadRequest`, `SubscribeRequest`, `Report`,
`WriteRequest`, `InvokeOptions`, `Result` and `StatusError`. Each type's
doc comment names the fields a host sets.

- **Read.** `Node.Read` applies the report to the image and returns it.
  Its paths are promoted fields (`req.Attributes = …`). Unless
  `IncludeKnownVersions` is set, the cached data versions go along
  as filters, so the peer sends only clusters that changed.
- **Write.** `Node.Write` takes `client.WriteRequest`s, each a path and a
  value. The value is a generated struct or request, a `spec.List`, or a
  scalar `spec.Value`: `spec.Int16(2250)`, `spec.Uint8(20)`,
  `spec.Uint(onoffdef.StartUpOnOffToggle)`, `spec.Null[uint16]()`.
  `DataVersion` makes a write conditional. The write goes to the peer
  only, and the image follows when the peer reports the new value. A write
  that does not fit one message goes in chunks, each answered by its own
  WriteResponse, the statuses concatenated; a timed write must fit one.
- **Invoke.** `Node.Invoke` takes the command path and the generated
  request type (`onoffdef.ToggleRequest{}`).
  `InvokeOptions{Timed: true}` sends a TimedRequest first.
- **Subscribe.** `Node.Subscribe` takes a `client.SubscribeRequest`, which
  embeds the paths as `ReadRequest` does (`req.Attributes = …`, or
  `client.SubscribeRequest{SubscribeRequest: client.SubscribePaths{…}}`);
  `IncludeKnownVersions` keeps the cached versions out of the first and
  every later request. It applies each report to the image before
  `Updated` sees it. `Updated` runs on the goroutine that reads the report,
  inside the report's exchange, so keep it short. The reference controller
  hands each report to a channel. `Sustain` re-establishes a subscription
  that was lost. `Subscription.Close` ends it, and so do the node's
  `Close` and `Disable`; `Done` is closed then. A subscription given up
  while the peer primes it is answered `INVALID_SUBSCRIPTION`, so the
  device keeps none for it.

*Source: `controller/client/surface.go`, `controller/client/interact.go:15-122`, `cluster/spec/value.go`, `client/interaction/subscribe.go:44-90`, `client/interaction/handler.go:126-138`, `examples/reference-controller/cli/interact.go:289-330`.*

## More of the facade

The calls below cover what a resident controller program (a daemon that
serves other clients) needs beyond pairing and the four interactions.
Each names its matter.js counterpart in its doc comment.

### Hooks per node

`Config.Node.Hooks` is one set of hooks for every node.
`Config.HooksFor(id)` returns the hooks of the node with that local id
instead, so a host attributes a report, an event or a subscription signal
to its node, as matter.js gives each paired node its own events.

### JSON values

A host that speaks JSON to its own clients reads and builds values
without a TLV type. `client.Value` is the decoded tree the role hands out
(an attribute value, an event's data, a command response's fields);
`client.Encodable` is what a write or an invoke takes. The type comes from
the generated cluster definitions:

| Call | The type of |
| --- | --- |
| `client.AttributeField(cluster, attribute)` | an attribute's value (a global attribute for any cluster) |
| `client.CommandField(cluster, command)` | a request command's fields |
| `client.ResponseField(cluster, command)` | a response command's fields |
| `client.EventField(cluster, event)` | an event's data |

`client.DecodeJSON(field, value, form)` renders a value in the forms of
matterjs-server's WebSocket API: `client.TagBased` keys a struct by its
field ids (`{"0": true}`, the attribute form), `client.NameBased` by its
camelCase names (`{"newPosition": 1}`, the form of command fields and
event data); bytes are standard base64, integers JSON numbers (64-bit ones
in full), lists arrays, null a nullable value's null; a member the model
does not name is left out. `client.EncodeJSON(field, raw)` reads either
key form (names compared without regard to case, or decimal ids) and
base64 bytes; null for an optional member that is not nullable counts as
left out, and a mandatory member a command's fields leave out takes the
specification's fallback, as matter.js's `MandatoryDefaultValue` computes
it (0, false, empty; an enum stays required). A value of an element no
definition names is untyped: a struct keyed by its context tags. A value
that does not fit is `client.ErrJSONValue`. Every cluster of the Matter
1.6.1 model has a definition, AccessControl, OperationalCredentials and
GroupKeyManagement included (their servers are hand-written; their
packages carry the definition and the typed state only), so an ACL entry
or a fabric list reads and writes typed. The JSON is written as
JavaScript's `JSON.stringify` writes it: `<`, `>` and `&` are not escaped,
and there is no trailing newline.

### Discovery

`Controller.Discover(ctx, id, timeout)` searches DNS-SD for the
commissionable devices a `discovery.Identifier` selects
(`ByLongDiscriminator`, `ByShortDiscriminator`, `ByVendor`,
`ByVendorProduct`, `ByDeviceType`, `ByInstanceID`) until the timeout passes
on the controller's clock (0 is 15 minutes, matter.js's default), and
returns every device found, each once. The end of the search is no error.
`Controller.DiscoverWith(ctx, id, timeout, controller.DiscoverOptions{First:
true})` returns the first device that answers with an address instead, as
soon as it answers — matter.js's `Peers.locate`; none before the timeout is
an empty result. The reference controller's `discover --first` uses it.

### Pairing by address

`PairOptions.Address` (a `*net.UDPAddr`) pairs a device at that address
without DNS-SD: no advertisement is needed, the code supplies the passcode
and its discriminator is not checked. `controller.DeviceAt(addr)` is the
`client.Device` of an address alone — no instance id, no TXT record — and
a supported input of `Registry.ForDevice`, which creates a new node for
it, as matter.js commissions a `knownAddress`. `Address` with `BLE` is
`controller.ErrConfiguration`.

### Pairing codes

`controller.ParsePairingCode(code)` decodes a QR code (`MT:…`) or a manual
code with matter.js's validation: `Manual`, `Version`, `VendorID`,
`ProductID`, `Flow`, `Capabilities` (the discovery bitmap), `Discriminator`
(the long one of a QR code, the short one of a manual code), `Passcode` and
`HasExtra` (the QR code carries TLV data). `PairingCode.Identifier()` is
the discovery selector. A code it refuses is `controller.ErrPairingCode`,
and `Pair` returns the same error for the same code.

### The commissioning window

`Node.OpenCommissioningWindow(ctx, timeout, client.WindowOptions{})` opens
an enhanced commissioning window on a paired node so another administrator
can add it, as matter.js's `openEnhancedCommissioningWindow` does: a window
open before is revoked, a random discriminator and passcode are drawn, the
PAKE verifier is derived over a random 32-byte salt at 1000 iterations, and
`OpenCommissioningWindow` is sent (timed). The `client.CommissioningWindow`
it returns carries the manual code, the QR code, the passcode, the long
discriminator, the node's vendor and product id and the timeout (0 is
`client.DefaultWindowTimeout`, 900 s). `WindowOptions{Basic: true}` opens a
basic window, in which the node takes its own passcode; a node without the
Basic feature is `client.ErrNoBasicWindow`.
`Node.RevokeCommissioningWindow(ctx)` closes a window and reports whether
one was open.

### Batched invokes

`Node.InvokeBatch(ctx, []client.CommandRequest, opts)` sends several
commands and returns one `Result` per command, in their order. The
commands go in batches of the peer's `Node.MaxPathsPerInvoke()` (its
session parameters; 1 when it reported none), each batch on an exchange of
its own, in parallel, as matter.js splits an invoke. A command's own status
is its `Result.Status` — `NoCommandResponse` for a command the peer did not
answer — and not an error; the error is an exchange that failed.

### OTA push

`controller.Config.OTA` (`controller.OTAConfig`) runs matter.js's
software-update manager: `Controller.SoftwareUpdates()` is a
`softwareupdate.Manager` — `AddUpdateConsent` queues a node's update and
announces the provider to it, `ForceUpdate` announces at once,
`RemoveConsent` cancels, `QueryUpdates` and `CheckForUpdates` look for
updates in the `softwareupdate.Images` store and the DCL ledgers,
`QueuedUpdates` shows the queue. The provider runs on the controller's own
node, which `OTAConfig.StartNode` starts (required;
`node/controllernode.Start`) in the controller's database and the
controller commissions into its fabric on first use; `Controller.OwnNode()`
returns it. The update needs the node's default subscription: the manager
judges a node on current data only.

```go
images := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
c, err := controller.New(ctx, controller.Config{ /* … */
	OTA: &controller.OTAConfig{Images: images, AllowTestImages: true, StartNode: controllernode.Start},
})
_, _ = images.Store(ctx, image, softwareupdate.ModeLocal, nil)
_, err = c.SoftwareUpdates().AddUpdateConsent(ctx, peer, softwareupdate.Target{
	VendorID: 0xFFF1, ProductID: 0x8000, TargetSoftwareVersion: 2,
})
```

### TCP

`controller.Config.TCP` turns the TCP initiator on (`Outgoing`) and a TCP
listener (`Incoming`): a peer that advertises a TCP server and accepts TCP
in its session parameters is reached over TCP when the controller prefers
it (`Config.TransportPreference`) or an interaction carries a large message
(`ClientRequest.LargeMessage`; an invoke that leaves it nil sets it when one
of its commands has matter.js's Large Message quality, as
`schema.CommandLargeMessage` reports it); the sessions of a dropped connection close. `Controller.TCPAddr()` is the listener's
address.

### Network profiles

`controller.Config.Networks` sets the network profiles a peer's
connections, exchanges and address probes are throttled by, per medium
(`peer.DefaultTemplates`: fast, wifi, thread, conservative, unknown,
unlimited, icdLit), and
`OwnNetworkProfile` the MRP margin our own messages carry. A peer of
unknown medium gets the conservative limits; an await-mode LIT ICD goes on
the unthrottled `icdLit` profile.

### The commissioner advertisement

`controller.Config.Advertise` (`controller.Advertise{Name, DeviceType,
VendorID, ProductID, PrivacyMasked}`) publishes the controller's
`_matterd._udp` record, so a device's commissioner discovery finds it.

### Completing a commissioning

`Registry.CompleteCommissioning(ctx, nodeID, client.CompleteOptions{…})`
finishes a commissioning another commissioner ran up to its operational
hand-off (`commissioner.Options.FinalizeCommissioning`): the node is
created on the shared fabric, started, and sent CommissioningComplete; any
failure before an Ok deletes it.

### The fabric label

`Registry.SetFabricLabel(ctx, label)` renames our fabric at run time:
the label (at most 32 characters, else `commissioner.ErrLabel`) is saved
with the fabric record (`commissioner.Fabric.SetLabel`), and every
connected node gets `UpdateFabricLabel` at once; a node that is not
connected gets it at its next start. A node that refuses the name because
another of its fabrics carries it is `commissioner.ErrFabricLabelConflict`;
the errors of the nodes not updated are joined. `Fabric.CurrentLabel()`
reads the label.

### The default subscription per node

`Node.SetAutoSubscribe(ctx, on)` switches a node's sustained default
subscription on or off and stores the choice with the node's settings: a
started node establishes or closes the subscription at once, a stopped one
follows the choice at its next start, as matter.js's `autoSubscribe` state
does. `Node.AutoSubscribe()` reads it.

### The specification version

`Node.Properties().Specification()` decodes the node's BasicInformation
SpecificationVersion into a `client.SpecVersion` (`Major`, `Minor`,
`Patch`, `Reserved`); `String()` renders it as `"1.4.0"`, `Uint32()`
gives the attribute value back for comparisons, and `client.SpecVersionOf`
decodes a value a host holds. An unknown or zero version reports false.

### Pairing over BLE

`PairOptions.BLE` is a Bluetooth LE central (`ble/bluez` on Linux,
`ble/esphome` through ESPHome Bluetooth proxies — [guide](../guides/ble-proxies.md) —,
`controller/ble/bletest` in a test): `Pair` then finds the device by its
BLE advertisement instead of DNS-SD, runs PASE over BTP, and the network
steps provision `PairOptions.WiFi` or `PairOptions.Thread` before CASE
finds the device on the network. A device on BLE has no network yet, so
one of the two is required. `PairOptions.WiFi` and `Thread` also provision
a device found over DNS-SD that advertises Wi-Fi or Thread.

### Thread datasets

`thread.DecodeDataset(b)` (package `controller/thread`) reads a Thread
operational dataset, as matter.js's `OperationalDataset.decode` does:
`NetworkName`, `ExtPANID`, `Channel`, `PANID` and every other MeshCoP TLV;
`DecodeDatasetHex` takes hex, and `Redact` drops the secrets for a log
line. The commissioner provisions the dataset as bytes
(`commissioner.ThreadNetwork`).

*Source: `controller/controller.go`, `controller/code.go`, `controller/client/window.go`, `controller/client/interact.go`, `controller/client/label.go`, `controller/commissioner/fabric.go`, `client/interaction/invoke_batch.go`, `controller/facade_test.go`.*

## What is not here yet

BLE on macOS and Windows (ADR 0024 decision 6) is outside the current
stages. Group messages need a node of the fabric: the reference
controller uses the controller's own node (`Controller.OwnNode`) for
`group` and `ota`. A batcher that joins single invokes into one
InvokeRequest, as matter.js's does within a timer tick, is not ported:
`Node.InvokeBatch` is the explicit form.

*Source: `controller/ota.go`, `controller/tcp.go`, `controller/advertise.go`, `controller/softwareupdate`, `node/controllernode`, `client/peer/network.go`, `controller/client/complete.go`, `examples/reference-controller/README.md` (The controller's own node).*
