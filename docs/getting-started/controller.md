# Getting started: the controller side

The controller role pairs Matter nodes onto a fabric this module owns. It
then reads, writes, invokes and subscribes on those nodes. It entered the
module in stages under [ADR 0020](../adr/0020-commissioner-and-controller-role.md)
and lives under `controller/`. This page builds the stack, pairs a node and
uses it. The program is `examples/quickstart-controller/main.go`. The
complete tool, with every command, is `examples/reference-controller`.

## The stack

A controller is assembled from the packages under `controller/`. The
reference controller's `openStack` builds them in this order:

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

The quickstart's `open` follows the same order, and its `close` tears the
stack down in reverse.

*Source: `examples/reference-controller/cli/stack.go:63-240`, `examples/quickstart-controller/main.go`.*

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
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "modernc.org/sqlite" // the host picks the SQLite driver

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// openNames opens the DNS-SD cache; a test replaces it.
var openNames = discovery.Open

// stack is the controller, in the order the reference controller's
// openStack builds it.
type stack struct {
	db     *sql.DB
	fabric *commissioner.Fabric
	mgr    *exchange.Manager
	names  *discovery.Names
	peers  *peer.Set
	subs   *interaction.Subscriptions
	com    *commissioner.Commissioner
	reg    *client.Registry
	stop   func()
}

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
	s, err := open(ctx, dbPath, "[::]:0", slog.Default())
	if err != nil {
		return err
	}
	defer s.close(context.WithoutCancel(ctx))
	if code != "" {
		if err := s.pair(ctx, code); err != nil {
			return err
		}
	}
	nodes := s.reg.Commissioned()
	if len(nodes) == 0 {
		return errors.New("no paired node: pass -code")
	}
	return use(ctx, nodes[0])
}

// open builds the stack over the database: the fabric (created on the first
// run, loaded afterwards), the socket, DNS-SD, peers, commissioner, registry.
func open(ctx context.Context, dbPath, listen string, log *slog.Logger) (_ *stack, err error) {
	s := &stack{stop: func() {}}
	defer func() {
		if err != nil {
			s.close(context.WithoutCancel(ctx))
		}
	}()
	if s.db, err = sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"); err != nil {
		return nil, err
	}
	if err := errors.Join(store.Apply(ctx, s.db), sqlstore.Apply(ctx, s.db)); err != nil {
		return nil, err
	}
	keep := ca.SettingStore{Settings: store.New(s.db)} // "controller.ca" and "controller.ca.fabric"
	authority, err := ca.New(ca.Config{Store: keep})
	if err != nil {
		return nil, err
	}
	// A first run draws the fabric id and our node id; NewFabric saves them.
	fcfg := commissioner.FabricConfig{VendorID: 0xFFF1, Label: "quickstart", Store: keep, FabricID: max(random(), 1), RootNodeID: random()%0xFFFF_FFEF_FFFF_FFFF + 1}
	if _, found, err := keep.LoadFabric(); err != nil { //nolint:contextcheck // the settings store's Load takes no context
		return nil, err
	} else if found {
		fcfg.FabricID, fcfg.RootNodeID = 0, 0 // the stored record decides
	}
	if s.fabric, err = commissioner.NewFabric(authority, fcfg); err != nil {
		return nil, err
	}
	l, err := udp.New(udp.Config{LocalAddr: listen})
	if err != nil {
		return nil, err
	}
	if s.mgr, err = exchange.NewManager(l, exchange.Config{Logger: log}); err != nil {
		_ = l.Close()
		return nil, err
	}
	serveCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	served := make(chan struct{})
	go func() { defer close(served); _ = l.Serve(serveCtx, s.mgr.HandleDatagram) }()
	s.stop = func() { cancel(); <-served }
	if s.names, err = openNames(log); err != nil {
		return nil, err
	}
	resumption := &peer.MemoryResumptionStore{}
	fabrics := []*commissioner.Fabric{s.fabric}
	if s.peers, err = peer.NewSet(peer.Context{Exchanges: s.mgr, Names: s.names, Fabrics: s.fabric.Lookup, Resumption: resumption, Logger: log}); err != nil {
		return nil, err
	}
	s.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: log})
	//nolint:contextcheck // the constructors take no context; the peers they adopt resolve on their own
	if s.com, err = commissioner.New(commissioner.Context{
		Exchanges: s.mgr, Peers: s.peers, Attestation: attestation.TestStore(), Logger: log,
		Subscriptions: s.subs, Fabrics: fabrics, Resumption: resumption,
	}); err != nil {
		return nil, err
	}
	s.reg, err = client.NewRegistry(client.RegistryConfig{ //nolint:contextcheck // as commissioner.New
		Store: sqlstore.New(s.db), Commissioner: s.com, Peers: s.peers, Exchanges: s.mgr,
		Fabrics: fabrics, Subscriptions: s.subs, Logger: log, Node: client.Config{Logger: log},
	})
	return s, err
}

// close tears the stack down in reverse order.
func (s *stack) close(ctx context.Context) {
	if s.reg != nil {
		_ = s.reg.Close(ctx)
	}
	if s.subs != nil {
		_ = s.subs.Close(ctx)
	}
	if s.com != nil {
		s.com.Close()
	}
	if s.peers != nil {
		s.peers.Close(ctx)
	}
	if s.mgr != nil {
		s.mgr.Close()
	}
	s.stop()
	if s.names != nil {
		_ = s.names.Close()
	}
	if s.db != nil {
		_ = s.db.Close()
	}
}

// pair finds the device by the code's discriminator over DNS-SD and
// commissions it: PASE, attestation against the CSA test PAAs, the NOC.
func (s *stack) pair(ctx context.Context, code string) error {
	var passcode uint32
	var id discovery.Identifier
	if qr, err := setup.DecodeQRCode(code); err == nil {
		passcode, id = qr.Passcode, discovery.ByLongDiscriminator(qr.Discriminator)
	} else if m, err := setup.DecodeManualCode(code); err == nil {
		passcode, id = m.Passcode, discovery.ByShortDiscriminator(m.ShortDiscriminator)
	} else {
		return fmt.Errorf("-code: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	sc := discovery.NewScanner(s.names)
	defer sc.Close()
	fctx, found := context.WithCancel(ctx)
	var dev *discovery.Device
	_, _ = sc.FindCommissionable(fctx, id, func(d discovery.Device) {
		if dev == nil && len(d.Addresses) > 0 {
			dev = &d
			found()
		}
	})
	found()
	if dev == nil {
		return errors.New("no commissionable device answered")
	}
	n, err := s.reg.ForDevice(ctx, *dev)
	if err != nil {
		return err
	}
	noSubscribe := false // this program exits; a resident host keeps the default subscription
	return s.reg.Commission(ctx, n, client.CommissionOptions{
		Options:       commissioner.Options{Passcode: passcode, OnAttestationFailure: attestation.Reject()},
		AutoSubscribe: &noSubscribe,
	})
}

// use reads, invokes and subscribes through the node's client image.
func use(ctx context.Context, n *client.Node) error {
	label := im.ConcreteAttributePath{Endpoint: 0, Cluster: basicinfo.ClusterID, Attribute: basicinfo.AttrNodeLabel, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	if _, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{label}}}); err != nil {
		return err
	}
	if ep := n.Endpoint(0); ep != nil && ep.Cluster(basicinfo.ClusterID) != nil {
		if v, err := ep.Cluster(basicinfo.ClusterID).Decoded(basicinfo.AttrNodeLabel); err == nil {
			fmt.Printf("NodeLabel: %v\n", v)
		}
	}
	toggle := im.ConcreteCommandPath{Endpoint: 1, Cluster: onoffdef.ClusterID, Command: onoffdef.CmdToggle, HasEndpoint: true, HasCluster: true, HasCommand: true}
	if _, err := n.Invoke(ctx, toggle, onoffdef.ToggleRequest{}, interaction.InvokeOptions{}); err != nil {
		return err
	}
	onOff := im.ConcreteAttributePath{Endpoint: 1, Cluster: onoffdef.ClusterID, Attribute: onoffdef.AttrOnOff, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	sub, err := n.Subscribe(ctx, interaction.SubscribeRequest{
		Attributes:         []im.ConcreteAttributePath{onOff},
		MaxIntervalCeiling: interaction.Interval(time.Minute),
		Updated: func(r *interaction.Report) { // keep it short: it runs on the report's exchange
			fmt.Printf("report: %d attribute(s)\n", len(r.Attributes))
		},
	})
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

func random() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}
```

```sh
go run ./examples/quickstart-controller -code MT:-24J0AFN00KA0648G00
go run ./examples/quickstart-controller          # the same node, next run
```

The test opens the stack twice on one database and checks that the second
run is the same fabric. It also checks that a malformed code is refused and
that a valid code that no device answers ends at the deadline.

*Source: `examples/quickstart-controller/main_test.go`.*

## The fabric

`ca.New` with a store keeps the root key and the certificate counter. The
CA saves after every certificate it issues, so serial numbers do not repeat
after a restart. `commissioner.NewFabric` with a store loads the stored
fabric record (fabric id, our node id, IPK, index, label, key and NOC). It
mints and saves a new record only when the store has none.

A stored record wins over the configuration. A non-zero `FabricID` or
`RootNodeID` that differs from the stored record is `ErrFabricMismatch`.
That is why the quickstart sets both fields to zero once a record exists.
`ca.SettingStore` keeps the CA under the settings key `controller.ca` and
the fabric under `controller.ca.fabric`.

The database therefore holds the fabric's private keys. Anyone with the
file controls every node paired to it.

*Source: `controller/ca/doc.go:4-17`, `controller/commissioner/fabric.go:23-69`, `controller/ca/persist.go:94-115`, `examples/reference-controller/README.md` (The database).*

## Pairing

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

The policy decides what a finding does:

| Policy | Effect on a finding |
| --- | --- |
| `attestation.Accept()` | proceeds; each finding is logged |
| `attestation.Reject()` | rejects the commissioning |
| `Policy{Decide: …}` | your function decides alone |
| zero value `Policy{}` | **proceeds**, with a warning per finding |

The zero value proceeds because matter.js does the same for backward
compatibility. Set the policy explicitly.

*Source: `controller/attestation/doc.go:4-20`, `controller/attestation/store.go:87-93`, `controller/attestation/policy.go:17-61`, `controller/commissioner/commissioner.go:52-54`, `controller/dcl/doc.go:4-24`, `examples/reference-controller/cli/stack.go:242-297`.*

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
| `Cluster.Decoded(attr)` | an attribute value, decoded through the definition |
| `Cluster.Value(attr)` | the raw TLV tree |

The image is persisted with `controller/client/sqlstore`, so a later run
starts from what the last one saw.

*Source: `controller/client/doc.go:19-50`, `controller/client/registry.go:24-192`, `controller/client/registry.go:345-371`, `controller/client/endpoint.go:56-148`, `controller/client/cluster.go:207-348`, `examples/reference-controller/cli/pair.go:191-212`.*

## Read, write, invoke, subscribe

All four go through the node and its CASE session. The peer set
establishes CASE when no session is live.

- **Read.** `Node.Read` applies the report to the image and returns it.
  Unless `IncludeKnownVersions` is set, the cached data versions go along
  as filters, so the peer sends only clusters that changed.
- **Write.** `Node.Write` takes `interaction.WriteRequest`s, each a path
  and a value. The value is a generated type, or a TLV tree through
  `interaction.Raw`. `DataVersion` makes a write conditional. The write
  goes to the peer only, and the image follows when the peer reports the
  new value.
- **Invoke.** `Node.Invoke` takes the command path and the generated
  request type (`onoffdef.ToggleRequest{}`).
  `InvokeOptions{Timed: true}` sends a TimedRequest first.
- **Subscribe.** `Node.Subscribe` applies each report to the image before
  `Updated` sees it. `Updated` runs on the goroutine that reads the report,
  inside the report's exchange, so keep it short. The reference controller
  hands each report to a channel. `Sustain` re-establishes a subscription
  that was lost.

*Source: `controller/client/interact.go:15-122`, `client/interaction/write.go:39-52`, `client/interaction/invoke.go:19-31`, `client/interaction/subscribe.go:44-90`, `client/interaction/handler.go:126-138`, `examples/reference-controller/cli/interact.go:289-330`.*

## What is not here yet

BLE on macOS and Windows (ADR 0024 decision 6) and TCP on the controller
side are outside the current stages. Group messages and OTA images need a node of
the fabric. The reference controller starts its own node for `group` and
`ota` and keeps it in the same database.

*Source: `controller/commissioner/doc.go:15-19`, `client/peer/doc.go:12-16`, `examples/reference-controller/README.md` (The controller's own node).*
