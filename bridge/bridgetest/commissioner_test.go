// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/tlv"
)

const (
	passcode      = 20202021
	discriminator = 3840
	fabricID      = 0x0000_0000_0000_0FAB
	deviceNodeID  = 0x0000_0000_0000_0D0D
)

// newDevice builds a node.Node on the database at path: the device the
// commissioner is tested against.
func newDevice(t *testing.T, adv mdns.Advertiser, path string) *node.Node {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := node.New(node.Config{
		Network:            node.Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Commissioning:      node.Commissioning{Passcode: passcode, Discriminator: discriminator, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric", ProductName: "bridgetest", NodeLabel: "device", SerialNumber: "BT-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:      node.Store{DB: db},
		Advertiser: adv,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("node.New: %v", err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n
}

// startNode builds and starts a device.
func startNode(t *testing.T, adv mdns.Advertiser, path string) *node.Node {
	t.Helper()
	n := newDevice(t, adv, path)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return n
}

func path(endpoint uint16, cluster, attribute uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: endpoint, Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func options() bridgetest.Options {
	return bridgetest.Options{Passcode: passcode, Discriminator: discriminator, FabricID: fabricID, NodeID: deviceNodeID, Seed: 1}
}

// TestCommissionerRefusals: what the commissioner reports when the device
// refuses it, or when it cannot find the device.
func TestCommissionerRefusals(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	n := startNode(t, adv, filepath.Join(t.TempDir(), "node.db"))
	ctx := context.Background()

	wrong := options()
	wrong.Passcode = 20202022
	if _, err := bridgetest.Dial(t, n.Bridge().LocalAddr(), wrong).Commission(ctx); !errors.Is(err, bridgetest.ErrHandshake) {
		t.Errorf("Commission with the wrong passcode = %v, want ErrHandshake", err)
	}

	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), options())
	if _, err := c.CASE(ctx); !errors.Is(err, bridgetest.ErrHandshake) {
		t.Errorf("CASE before AddNOC = %v, want ErrHandshake", err)
	}

	elsewhere := options()
	elsewhere.Advertiser, elsewhere.Discriminator = adv, discriminator+1
	if _, err := bridgetest.Dial(t, n.Bridge().LocalAddr(), elsewhere).PASE(ctx); err == nil {
		t.Error("PASE found a commissionable record for a discriminator nobody advertises")
	}
	elsewhere.Discriminator = discriminator
	if _, err := bridgetest.Dial(t, n.Bridge().LocalAddr(), elsewhere).CASE(ctx); err == nil {
		t.Error("CASE found an operational record before AddNOC")
	}

	if _, err := c.ResolveOperational(ctx, c.CompressedFabricID(), 0x99); err == nil {
		t.Error("ResolveOperational answered for a controller that never had an identity")
	}
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := c.Accept(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Accept with no handshake = %v", err)
	}
}

// TestCommissionerSessions commissions a node through its DNS-SD records
// and exercises the session API over CASE.
func TestCommissionerSessions(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	n := startNode(t, adv, filepath.Join(t.TempDir(), "node.db"))
	ctx := context.Background()
	opts := options()
	opts.Advertiser = adv
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	s, err := c.Commission(ctx)
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	if s.PeerNodeID() != deviceNodeID || s.LocalNodeID() != bridgetest.DefaultControllerNodeID {
		t.Errorf("session nodes %X → %X", s.LocalNodeID(), s.PeerNodeID())
	}
	if addrs, err := c.ResolveOperational(ctx, c.CompressedFabricID(), bridgetest.DefaultControllerNodeID); err != nil || len(addrs) != 1 || addrs[0].String() != c.Addr().String() {
		t.Errorf("ResolveOperational = %v, %v", addrs, err)
	}

	// A wildcard read of the root spans several chunks.
	all := im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true}
	report, err := s.Read(ctx, all)
	if err != nil {
		t.Fatalf("wildcard read: %v", err)
	}
	if v, ok := report.Value(0, 0x0028, 0x0005); !ok || v.String != "device" {
		t.Errorf("NodeLabel %+v (%v) among %d attributes", v, ok, len(report.Attributes))
	}
	if _, ok := report.Value(9, 0x0028, 0x0005); ok {
		t.Error("Value found an attribute of an endpoint the report does not carry")
	}
	missing, err := s.Read(ctx, path(0, 0x0028, 0xFFF0))
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := missing.Status(0, 0x0028, 0xFFF0); !ok || st != im.StatusUnsupportedAttribute {
		t.Errorf("unsupported attribute status %s (%v)", st, ok)
	}
	if _, ok := missing.Status(0, 0x0028, 0x0005); ok {
		t.Error("Status found an attribute the report does not carry")
	}

	// A command the cluster lacks answers with a status.
	res, err := s.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0028, Command: 0x7F}, nil)
	if err != nil || res.Status == im.StatusSuccess {
		t.Errorf("unknown command = %+v, %v", res, err)
	}
	// A subscription nothing matches ends with a StatusResponse.
	var statusErr *bridgetest.StatusError
	if _, err := s.Subscribe(ctx, 0, 30, im.ConcreteAttributePath{Cluster: 0xFFF1FC00, HasCluster: true}); !errors.As(err, &statusErr) || statusErr.Status != im.StatusInvalidAction {
		t.Errorf("subscription to nothing = %v", err)
	} else if statusErr.Error() == "" {
		t.Error("StatusError without a message")
	}
	// A second controller the ACL does not name.
	other, err := c.CASEAs(ctx, 0x0000_0000_0000_0042, 0x0001_0001)
	if err != nil {
		t.Fatalf("CASEAs: %v", err)
	}
	denied, err := other.Read(ctx, path(0, 0x0028, 0x0005))
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := denied.Status(0, 0x0028, 0x0005); st != im.StatusUnsupportedAccess {
		t.Errorf("unlisted controller read status %s", st)
	}
	label := func(e *tlv.Encoder, tag tlv.Tag) { e.PutUTF8(tag, "renamed") }
	if st, err := other.Write(ctx, path(0, 0x0028, 0x0005), label); err != nil || st != im.StatusUnsupportedAccess {
		t.Errorf("unlisted controller write = %s, %v", st, err)
	}
	if st, err := s.Write(ctx, path(0, 0x0028, 0x0005), label); err != nil || st != im.StatusSuccess {
		t.Errorf("administrator write = %s, %v", st, err)
	}
	if v, _ := mustRead(t, s, path(0, 0x0028, 0x0005)).Value(0, 0x0028, 0x0005); v.String != "renamed" {
		t.Errorf("NodeLabel after the write = %q", v.String)
	}
	other.Close()
	other.Close()
	if _, err := other.Read(ctx, path(0, 0x0028, 0x0005)); !errors.Is(err, bridgetest.ErrSessionClosed) {
		t.Errorf("read on a closed session = %v", err)
	}
	if _, err := other.Write(ctx, path(0, 0x0028, 0x0005), label); !errors.Is(err, bridgetest.ErrSessionClosed) {
		t.Errorf("write on a closed session = %v", err)
	}
	if _, err := other.Invoke(ctx, im.ConcreteCommandPath{Cluster: 0x0028, Command: 0}, nil); !errors.Is(err, bridgetest.ErrSessionClosed) {
		t.Errorf("invoke on a closed session = %v", err)
	}
	if _, err := other.Subscribe(ctx, 0, 30, path(0, 0x0028, 0x0005)); !errors.Is(err, bridgetest.ErrSessionClosed) {
		t.Errorf("subscribe on a closed session = %v", err)
	}

	if status, err := c.RemoveFabric(ctx, s); err != nil || status != 0 {
		t.Fatalf("RemoveFabric = %d, %v", status, err)
	}
	if fabrics, _ := n.Fabrics(ctx); len(fabrics) != 0 {
		t.Errorf("fabrics after RemoveFabric: %+v", fabrics)
	}
}

// TestCommissionerAcceptsReestablishment: after a restart the node opens
// CASE to the commissioner, which answers as the responder, and the
// former subscription's reports arrive on the accepted session.
func TestCommissionerAcceptsReestablishment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "node.db")
	first := startNode(t, nil, db)
	c := bridgetest.Dial(t, first.Bridge().LocalAddr(), options())
	s, err := c.Commission(ctx)
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	sub, err := s.Subscribe(ctx, 0, 60, path(0, 0x0028, 0x0005))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if v, _ := sub.Priming.Value(0, 0x0028, 0x0005); v.String != "device" || sub.MaxInterval == 0 {
		t.Errorf("priming %+v, max interval %d", v, sub.MaxInterval)
	}
	waitPersisted(t, first)
	if err := first.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	second := newDevice(t, nil, db)
	second.Bridge().AttachOperationalResolver(c)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	resumed, err := c.Accept(ctx)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if resumed.PeerNodeID() != deviceNodeID {
		t.Errorf("accepted session from node %X", resumed.PeerNodeID())
	}
	priming, err := resumed.NextReport(ctx)
	if err != nil {
		t.Fatalf("NextReport: %v", err)
	}
	if priming.SubscriptionID != sub.ID {
		t.Errorf("priming report of subscription %d, want %d", priming.SubscriptionID, sub.ID)
	}
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := resumed.NextReport(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("NextReport with nothing due = %v", err)
	}
	resumed.Close()
}

// TestPASESession: a PASE session reads over the commissioning window, a
// subscription on it is refused — matter.js's client subscribes only to a
// peer with a peer address (ClientInteraction.ts:991-994 at 0ceb202d) —
// and a closed one answers ErrSessionClosed.
func TestPASESession(t *testing.T) {
	t.Parallel()
	n := startNode(t, nil, filepath.Join(t.TempDir(), "node.db"))
	ctx := context.Background()
	s, err := bridgetest.Dial(t, n.Bridge().LocalAddr(), options()).PASE(ctx)
	if err != nil {
		t.Fatalf("PASE: %v", err)
	}
	if s.PeerNodeID() != 0 || s.LocalNodeID() != 0 {
		t.Errorf("PASE session nodes %X → %X, want none", s.LocalNodeID(), s.PeerNodeID())
	}
	if v, _ := mustRead(t, s, path(0, 0x0028, 0x0005)).Value(0, 0x0028, 0x0005); v.String != "device" {
		t.Errorf("NodeLabel over PASE = %q", v.String)
	}
	if _, err := s.Subscribe(ctx, 0, 30, path(0, 0x0028, 0x0005)); !errors.Is(err, interaction.ErrUncommissioned) {
		t.Errorf("Subscribe over PASE = %v, want interaction.ErrUncommissioned", err)
	}
	s.Close()
	s.Close()
	if _, err := s.Read(ctx, path(0, 0x0028, 0x0005)); !errors.Is(err, bridgetest.ErrSessionClosed) {
		t.Errorf("read on a closed PASE session = %v", err)
	}
}

// TestControllerIdentities: the fabric's own identity is ControllerNodeID's
// with the fabric's CATs, another controller's is minted once, both carry
// the fabric's ids and the operational IPK, and ResolveSigma1Destination
// finds the identity a Sigma1 addresses.
func TestControllerIdentities(t *testing.T) {
	t.Parallel()
	opts := options()
	opts.CAT = []uint32{0x0001_0001}
	c := bridgetest.Dial(t, "127.0.0.1:9", opts)
	root := c.RootPublicKey()
	if len(root) != 65 || root[0] != 0x04 {
		t.Fatalf("root public key %X", root)
	}
	own := c.ControllerIdentity(bridgetest.DefaultControllerNodeID)
	other := c.ControllerIdentity(0x42, 0x0002_0001)
	if again := c.ControllerIdentity(0x42); !bytes.Equal(again.NOC, other.NOC) {
		t.Error("a second ControllerIdentity minted another NOC")
	}
	for _, id := range []sigma.Identity{own, other} {
		if id.FabricID != fabricID || id.CompressedFabricID != c.CompressedFabricID() || id.FabricIndex != 1 || id.PrivateKey == nil {
			t.Errorf("identity of node %X: %+v", id.NodeID, id)
		}
		if id.IPK != own.IPK {
			t.Errorf("identity of node %X has another IPK", id.NodeID)
		}
	}
	var random [sigma.RandomSize]byte
	random[0] = 7
	for _, id := range []sigma.Identity{own, other} {
		got, verifier, ok := c.ResolveSigma1Destination(sigma.ComputeDestinationID(id.IPK, random, root, fabricID, id.NodeID), random)
		if !ok || got.NodeID != id.NodeID || verifier == nil {
			t.Errorf("ResolveSigma1Destination for node %X = %+v, %v", id.NodeID, got, ok)
		}
	}
	if _, _, ok := c.ResolveSigma1Destination([32]byte{1}, random); ok {
		t.Error("ResolveSigma1Destination resolved a destination of no identity")
	}
}

func mustRead(t *testing.T, s *bridgetest.Session, p im.ConcreteAttributePath) *bridgetest.Report {
	t.Helper()
	r, err := s.Read(context.Background(), p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return r
}

// fatalTB stops a Dial at its first Fatal, so a test can see which
// options Dial refuses.
type fatalTB struct {
	testing.TB
}

type fatal struct{ msg string }

func (fatalTB) Fatal(args ...any)                 { panic(fatal{msg: fmt.Sprint(args...)}) }
func (fatalTB) Fatalf(format string, args ...any) { panic(fatal{msg: fmt.Sprintf(format, args...)}) }

// TestDialRefusesIncompleteOptions: Passcode, FabricID and NodeID are
// required, and the address must resolve.
func TestDialRefusesIncompleteOptions(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		addr   string
		mutate func(*bridgetest.Options)
	}{
		"passcode": {"127.0.0.1:5540", func(o *bridgetest.Options) { o.Passcode = 0 }},
		"fabric":   {"127.0.0.1:5540", func(o *bridgetest.Options) { o.FabricID = 0 }},
		"node":     {"127.0.0.1:5540", func(o *bridgetest.Options) { o.NodeID = 0 }},
		"address":  {"127.0.0.1:not-a-port", func(*bridgetest.Options) {}},
	} {
		opts := options()
		tc.mutate(&opts)
		func() {
			defer func() {
				if _, ok := recover().(fatal); !ok {
					t.Errorf("%s: Dial did not fail the test", name)
				}
			}()
			bridgetest.Dial(fatalTB{t}, tc.addr, opts)
		}()
	}
}

// TestDecodeTLV covers the decoder's refusals.
func TestDecodeTLV(t *testing.T) {
	t.Parallel()
	if _, err := bridgetest.DecodeTLV(nil); err == nil {
		t.Error("DecodeTLV(nil) succeeded")
	}
	if _, err := bridgetest.DecodeTLV([]byte{0x15, 0x24, 0x00}); err == nil { // a structure cut short
		t.Error("DecodeTLV of a truncated structure succeeded")
	}
	tree, err := bridgetest.DecodeTLV([]byte{0x15, 0x24, 0x01, 0x07, 0x18})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := tree.Child(1); !ok || v.Uint != 7 {
		t.Errorf("Child(1) = %+v (%v)", v, ok)
	}
	if _, ok := tree.Child(2); ok {
		t.Error("Child(2) found a missing field")
	}
}

// waitPersisted waits until n has stored its subscription for
// re-establishment, which it does after it sent the SubscribeResponse: a
// Stop before that ends the subscription with the run.
func waitPersisted(t *testing.T, n *node.Node) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows, err := n.Store().LoadServerSubscriptions(context.Background())
		if err == nil && len(rows) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the subscription was not persisted: %d rows, %v", len(rows), err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
