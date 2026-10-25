// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/dcl"
	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// Settings keys of the database (store.Store's matter_settings table).
const (
	caKey        = "controller.ca" // the CA; the fabric record is under caKey+".fabric"
	dclTrustKey  = "controller.dcl.trust"
	selfKey      = "controller.self"
	groupKeyPref = "controller.group."
)

// The fabric a first run creates. Mirrors matter.js
// packages/protocol/src/fabric/FabricAuthority.ts:133-171 (createFabric) at
// 0ceb202d: a random operational root node id (:134), a random 64-bit
// fabric id (:155), the test vendor 0xFFF1 when none is configured
// (:47, :137-140). The label is this tool's.
const (
	fabricVendorID = 0xFFF1
	fabricLabel    = "go-fabric controller"
)

// Operational node id range — matter.js packages/types/src/datatype/
// NodeId.ts:27-28 at 0ceb202d.
const operationalNodeMax = 0xFFFF_FFEF_FFFF_FFFF

// hooks are the seams a test replaces: the DNS-SD socket of the
// controller and the own node's advertiser and group transport.
var hooks struct {
	names func(log *slog.Logger) (*discovery.Names, error)
}

// stack is the controller: database, CA, fabric, socket, DNS-SD cache, peer
// set, subscriptions, commissioner and registry — in the order
// controller/client/registry_live_test.go:82-162 builds them.
type stack struct {
	e        *env
	db       *sql.DB
	settings *store.Store
	fabric   *commissioner.Fabric
	mgr      *exchange.Manager
	names    *discovery.Names
	set      *peer.Set
	subs     *interaction.Subscriptions
	com      *commissioner.Commissioner
	reg      *client.Registry
	certs    *dcl.CertificateStore
	policy   attestation.Policy

	stopServe func()
	served    chan struct{}
	self      *selfNode
}

// openDB opens the database and applies the node store's and the client
// registry's schemas (both idempotent).
func openDB(ctx context.Context, path string) (*sql.DB, error) {
	db, err := controller.OpenDB(path)
	if err != nil {
		return nil, err
	}
	if err := store.Apply(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := sqlstore.Apply(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return db, nil
}

func randomUint64() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	return binary.BigEndian.Uint64(b[:])
}

// isIPv4Listen reports whether a listen address names an IPv4 host.
func isIPv4Listen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil
}

// openStack builds the controller stack over the database.
//
//nolint:funlen // the stack is built in one place, in order
func openStack(e *env) (_ *stack, err error) {
	s := &stack{e: e}
	defer func() {
		if err != nil {
			s.close()
		}
	}()
	if s.db, err = openDB(e.ctx, e.opts.db); err != nil {
		return nil, err
	}
	s.settings = store.New(s.db)
	if s.policy, err = attestationPolicy(e); err != nil {
		return nil, err
	}
	cs := ca.SettingStore{Settings: s.settings, Key: caKey}
	rootSigner, err := openCAKey(e, cs)
	if err != nil {
		return nil, err
	}
	authority, err := ca.New(ca.Config{Store: cs, RootSigner: rootSigner})
	if err != nil {
		return nil, err
	}
	fcfg := commissioner.FabricConfig{VendorID: fabricVendorID, Label: fabricLabel, Store: cs}
	if _, ok, lerr := cs.LoadFabric(); lerr != nil {
		return nil, lerr
	} else if !ok {
		fcfg.FabricID = randomUint64()
		for fcfg.FabricID == 0 {
			fcfg.FabricID = randomUint64()
		}
		fcfg.RootNodeID = randomUint64()%operationalNodeMax + 1
	}
	if s.fabric, err = commissioner.NewFabric(authority, fcfg); err != nil {
		return nil, err
	}

	l, err := udp.New(udp.Config{LocalAddr: e.opts.listen, PreferIPv4: isIPv4Listen(e.opts.listen)})
	if err != nil {
		return nil, err
	}
	if s.mgr, err = exchange.NewManager(l, exchange.Config{Logger: e.log}); err != nil {
		_ = l.Close()
		return nil, err
	}
	serveCtx, stop := context.WithCancel(context.Background())
	s.stopServe, s.served = stop, make(chan struct{})
	go func() {
		defer close(s.served)
		_ = l.Serve(serveCtx, s.mgr.HandleDatagram)
	}()
	openNames := discovery.Open
	if hooks.names != nil {
		openNames = hooks.names
	}
	if s.names, err = openNames(e.log); err != nil {
		return nil, fmt.Errorf("DNS-SD: %w", err)
	}

	resumption := &peer.MemoryResumptionStore{}
	if s.set, err = peer.NewSet(peer.Context{Exchanges: s.mgr, Names: s.names, Fabrics: s.fabric.Lookup, Resumption: resumption, Logger: e.log}); err != nil {
		return nil, err
	}
	s.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: e.log})
	mode, err := attestation.ParseTrustMode(e.opts.trust)
	if err != nil {
		return nil, fmt.Errorf("--trust: %w", err)
	}
	snap, err := openSnapshot(e)
	if err != nil {
		return nil, err
	}
	if s.certs, err = openTrust(e, s.settings); err != nil {
		return nil, err
	}
	var online attestation.PAAStore
	if s.certs != nil {
		online = s.certs
	}
	trust := attestation.Trust(mode, snap, online)
	if s.com, err = commissioner.New(commissioner.Context{
		Exchanges: s.mgr, Peers: s.set, Attestation: trust, Logger: e.log,
		Subscriptions: s.subs, Fabrics: []*commissioner.Fabric{s.fabric}, Resumption: resumption,
	}); err != nil {
		return nil, err
	}
	if s.reg, err = client.NewRegistry(client.RegistryConfig{
		Store: sqlstore.New(s.db), Commissioner: s.com, Peers: s.set, Exchanges: s.mgr,
		Fabrics: []*commissioner.Fabric{s.fabric}, Subscriptions: s.subs, Logger: e.log,
		Node: client.Config{Logger: e.log},
	}); err != nil {
		return nil, err
	}
	return s, nil
}

// close tears the stack down in the reverse order
// (registry_live_test.go:129-139).
func (s *stack) close() {
	ctx := context.Background()
	if s.self != nil {
		s.self.stop()
	}
	if s.reg != nil {
		_ = s.reg.Close(ctx)
	}
	if s.subs != nil {
		_ = s.subs.Close(ctx)
	}
	if s.com != nil {
		s.com.Close()
	}
	if s.set != nil {
		s.set.Close(ctx)
	}
	if s.mgr != nil {
		s.mgr.Close()
	}
	if s.stopServe != nil {
		s.stopServe()
		<-s.served
	}
	if s.names != nil {
		_ = s.names.Close()
	}
	if s.certs != nil {
		s.certs.Close()
	}
	if s.db != nil {
		_ = s.db.Close()
	}
}

// openSnapshot is the DCL snapshot: --trust-dir's, else the embedded one.
// Its fetch time goes to the debug log, so a stale set is noticed.
func openSnapshot(e *env) (*snapshot.Snapshot, error) {
	var snap *snapshot.Snapshot
	var err error
	where := "embedded"
	if e.opts.trustDir != "" {
		where = e.opts.trustDir
		snap, err = snapshot.Load(os.DirFS(e.opts.trustDir))
	} else {
		snap, err = snapshot.Embedded()
	}
	if snap == nil {
		return nil, fmt.Errorf("DCL snapshot %s: %w", where, err)
	}
	if err != nil {
		// A malformed entry ends its kind's load; what came before is used.
		e.log.Warn("controller.dcl_snapshot.malformed", "from", where, "err", err)
	}
	src := snap.Source()
	paas, cds := snap.Len()
	e.log.Debug("controller.dcl_snapshot", "from", where, "ledger", src.Ledger, "fetched", src.BuiltAt.Format(time.RFC3339),
		"paas", paas, "cd_signers", cds, "skipped", snap.Skipped())
	return snap, nil
}

// openTrust is the online DCL trust store --dcl names, nil for off (the
// snapshot and, with --trust development, the test PAAs are used then). What it asks the ledger is in README.md.
func openTrust(e *env, settings *store.Store) (*dcl.CertificateStore, error) {
	var c *dcl.Client
	testnet := false
	switch e.opts.dcl {
	case "", "off":
		return nil, nil
	case "prod", "testnet":
		if !e.attests {
			return nil, nil
		}
	}
	switch e.opts.dcl {
	case "prod":
		c = dcl.NewClient(dcl.ProductionURL)
	case "testnet":
		c, testnet = dcl.NewClient(dcl.TestNetURL), true
	default:
		return nil, fmt.Errorf("--dcl %q, want prod, testnet or off", e.opts.dcl)
	}
	return dcl.OpenCertificateStore(e.ctx, dcl.CertificateOptions{
		Client: c, FetchTestCertificates: testnet,
		Store: dcl.SettingStore{Settings: settings, Key: dclTrustKey}, Logger: e.log,
	})
}

// attestationPolicy is --attestation: accept, reject, or ask on the
// console; unset is ask on a terminal and reject otherwise.
func attestationPolicy(e *env) (attestation.Policy, error) {
	mode := e.opts.attestation
	if mode == "" {
		mode = "reject"
		if isTerminal(e.stdin) {
			mode = "ask"
		}
	}
	switch mode {
	case "accept":
		return attestation.Accept(), nil
	case "reject":
		return attestation.Reject(), nil
	case "ask":
		return attestation.Policy{Decide: func(findings []attestation.Finding) (bool, string, error) {
			for _, f := range findings {
				e.printf("attestation finding: %s: %s\n", f.Check, f.Message)
			}
			if confirm(e, "Commission the device anyway?") {
				return true, "", nil
			}
			return false, "attestation findings rejected on the console", nil
		}}, nil
	}
	return attestation.Policy{}, fmt.Errorf("--attestation %q, want accept, reject or ask", mode)
}

// errUnknownNode is a node argument the registry does not hold.
var errUnknownNode = errors.New("unknown node")

// parseNodeID reads a node id, decimal or 0x hex.
func parseNodeID(s string) (uint64, error) {
	id, err := strconv.ParseUint(s, 0, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("node id %q", s)
	}
	return id, nil
}

// node finds a paired node by its node id or the registry's local id
// ("peer1").
func (s *stack) node(arg string) (*client.Node, uint64, error) {
	if strings.HasPrefix(arg, "peer") {
		if n := s.reg.Get(arg); n != nil {
			addr, _ := n.PeerAddress()
			return n, addr.NodeID, nil
		}
		return nil, 0, fmt.Errorf("%w %q", errUnknownNode, arg)
	}
	id, err := parseNodeID(arg)
	if err != nil {
		return nil, 0, err
	}
	n := s.reg.GetByAddress(s.fabric.AddressOf(id))
	if n == nil {
		return nil, 0, fmt.Errorf("%w 0x%016X: pair it first (nodes lists the paired ones)", errUnknownNode, id)
	}
	return n, id, nil
}

// withStack opens the stack, runs f, closes the stack.
func withStack(e *env, f func(s *stack) error) error {
	s, err := openStack(e)
	if err != nil {
		return err
	}
	defer s.close()
	return f(s)
}

// timeoutCtx is the command's network deadline (--timeout, default 60s).
func (e *env) timeoutCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(e.ctx, e.opts.timeout.or(defaultTimeout))
}
