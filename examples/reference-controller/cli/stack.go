// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/dcl"
	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// Settings keys of the database (store.Store's matter_settings table).
// The CA and the fabric record are controller.New's, under
// ca.DefaultSettingKey ("controller.ca", the fabric under
// "controller.ca.fabric"), the keys this tool always used.
const (
	dclTrustKey  = "controller.dcl.trust"
	selfKey      = "controller.self"
	groupKeyPref = "controller.group."
)

// The fabric a first run creates: controller.New's random fabric and root
// node id under the test vendor 0xFFF1; the label is this tool's.
const (
	fabricVendorID = controller.DefaultVendorID
	fabricLabel    = "go-fabric controller"
)

// hooks are the seams a test replaces: the DNS-SD socket of the
// controller and the own node's advertiser and group transport.
var hooks struct {
	names func(log *slog.Logger) (*discovery.Names, error)
}

// stack is the controller (controller.New over the database: CA, fabric,
// socket, DNS-SD, peers, commissioner, registry), the trust store this
// tool composes for it, and the own node.
type stack struct {
	e        *env
	db       *sql.DB
	settings *store.Store
	ctl      *controller.Controller
	fabric   *commissioner.Fabric
	set      *peer.Set
	com      *commissioner.Commissioner
	reg      *client.Registry
	certs    *dcl.CertificateStore
	policy   attestation.Policy

	self *selfNode
	// images is the own node's image store; onStatus the serving
	// command's observer of the update statuses, under statusMu.
	images   *softwareupdate.Images
	statusMu sync.Mutex
	onStatus func(p softwareupdate.Peer, st softwareupdate.Status, v uint32)
	// restore undoes what a command changed on a node for its run.
	restore []func()
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

// isIPv4Listen reports whether a listen address names an IPv4 host.
func isIPv4Listen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil
}

// openStack builds the controller over the database: the trust set
// --trust, --trust-dir and --dcl name, the CA root key --ca-key names, the
// socket --listen names, then controller.New, which creates or loads the
// fabric and builds the rest. The nodes stay stopped (ManualStart): each
// command connects the node it acts on.
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
	rootSigner, err := openCAKey(e, ca.SettingStore{Settings: s.settings})
	if err != nil {
		return nil, err
	}
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
	// The socket is this tool's, for --listen's IPv4 preference;
	// controller.New serves it and closes it.
	l, err := udp.New(udp.Config{LocalAddr: e.opts.listen, PreferIPv4: isIPv4Listen(e.opts.listen)})
	if err != nil {
		return nil, err
	}
	openNames := discovery.Open
	if hooks.names != nil {
		openNames = hooks.names
	}
	cfg := controller.Config{
		DB: s.db, Attestation: attestation.Trust(mode, snap, online), Listener: l,
		VendorID: fabricVendorID, Label: fabricLabel, CAKey: rootSigner,
		Logger: e.log, Node: client.Config{Logger: e.log}, ManualStart: true, Names: openNames,
	}
	if e.ownNode {
		if err := migrateSelfRecord(e.ctx, s.settings); err != nil {
			_ = l.Close()
			return nil, err
		}
		cfg.OTA = s.ownNodeConfig()
	}
	s.ctl, err = controller.New(e.ctx, cfg)
	if err != nil {
		return nil, err // New closed the socket
	}
	s.fabric, s.set, s.com, s.reg = s.ctl.Fabric(), s.ctl.Peers(), s.ctl.Commissioner(), s.ctl.Registry()
	return s, nil
}

// close tears the stack down: the controller (with its own node), the
// trust store, the database.
func (s *stack) close() {
	ctx := context.Background()
	if s.ctl != nil {
		_ = s.ctl.Close(ctx)
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
