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
