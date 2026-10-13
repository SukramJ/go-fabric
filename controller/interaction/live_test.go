// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/udp"
)

const (
	liveFabricID     uint64 = 0x0000_0000_0000_0F0C
	liveController   uint64 = 112233
	liveNode         uint64 = 0x0000_0000_0000_4322
	liveDiscriminate uint16 = 3840
)

// plug is a bridged OnOffPlugInUnit holding an onoff.Server.
type plug struct{ srv *onoff.Server }

func (p *plug) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey("plug:1"), DeviceAddress: "plug:1", ChannelAddress: "plug:1:0",
		DeviceType: onoff.DeviceTypeOnOffPlugInUnit, FriendlyName: "plug", Source: p,
	}
}

func (p *plug) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffPlugInUnit }

func (p *plug) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p.srv}
}

// liveNodeAt builds a node with one plug on the database at path, the
// controller's address as its operational resolver's answer.
func liveNodeAt(t *testing.T, path string, controllerAddr func() *net.UDPAddr) *node.Node {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := node.New(node.Config{
		Network:            node.Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Commissioning:      node.Commissioning{Passcode: testPasscode, Discriminator: liveDiscriminate, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "interaction live test", NodeLabel: "live node", SerialNumber: "IL-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:      node.Store{DB: db},
		Advertiser: mdns.NewNoop(),
		Logger:     discardLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(context.Background(), &plug{srv: srv}); err != nil {
		t.Fatal(err)
	}
	n.Bridge().AttachOperationalResolver(resolverFunc(func(context.Context, [8]byte, uint64) ([]*net.UDPAddr, error) {
		return []*net.UDPAddr{controllerAddr()}, nil
	}))
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n
}

type resolverFunc func(ctx context.Context, cfid [8]byte, nodeID uint64) ([]*net.UDPAddr, error)

func (f resolverFunc) ResolveOperational(ctx context.Context, cfid [8]byte, nodeID uint64) ([]*net.UDPAddr, error) {
	return f(ctx, cfid, nodeID)
}

// live is a node commissioned by the module's own commissioner, whose
// context carries the subscription registry and the CASE responder (§4).
type live struct {
	node   *node.Node
	dbPath string
	mgr    *exchange.Manager
	addr   *net.UDPAddr
	reg    *interaction.Subscriptions
	com    *commissioner.Commissioner
	set    *peer.Set
	peer   *peer.Peer
	c      *interaction.Interaction
	plugEP uint16
}

func newLive(t *testing.T, regClock exchange.Clock) *live {
	t.Helper()
	return newLiveWith(t, regClock, discardLog())
}

// newLiveWith is newLive with the registry's logger.
func newLiveWith(t *testing.T, regClock exchange.Clock, regLog *slog.Logger) *live {
	t.Helper()
	lv := &live{dbPath: filepath.Join(t.TempDir(), "node.db")}
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	lv.addr = l.LocalAddr()
	lv.node = liveNodeAt(t, lv.dbPath, func() *net.UDPAddr { return lv.addr })
	mgr, err := exchange.NewManager(l, exchange.Config{Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	lv.mgr = mgr
	serveCtx, stop := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(serveCtx, mgr.HandleDatagram)
	}()
	conn := &fakeMDNS{out: make(chan mdns.QueryResponse, 64)}
	names := discovery.NewNames(conn, discardLog())
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: liveFabricID, RootNodeID: liveController, VendorID: 0xFFF1, Label: "live"})
	if err != nil {
		t.Fatal(err)
	}
	resumption := &peer.MemoryResumptionStore{}
	lv.set, err = peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: resumption, Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	lv.reg = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: regClock, Logger: regLog})
	lv.com, err = commissioner.New(commissioner.Context{
		Exchanges: mgr, Peers: lv.set, Attestation: attestation.TestStore(), Logger: discardLog(),
		Subscriptions: lv.reg, Fabrics: []*commissioner.Fabric{fabric}, Resumption: resumption,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = lv.reg.Close(context.Background())
		lv.com.Close()
		lv.set.Close(context.Background())
		mgr.Close()
		stop()
		<-served
		_ = names.Close()
	})
	nodeAddr, err := net.ResolveUDPAddr("udp", lv.node.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	// The node's operational record, for the commissioning's CASE step.
	op := discovery.OperationalQName(fabric.Identity.CompressedFabricID, liveNode)
	hdr := func(name string, typ uint16) dns.RR_Header {
		return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: typ, Class: dns.ClassINET | 0x8000, Ttl: 120}
	}
	msg := new(dns.Msg)
	msg.Response = true
	msg.Answer = []dns.RR{
		&dns.SRV{Hdr: hdr(op, dns.TypeSRV), Target: "live-node.local.", Port: uint16(nodeAddr.Port)}, //nolint:gosec // a test port
		&dns.TXT{Hdr: hdr(op, dns.TypeTXT), Txt: []string{"SII=500", "SAI=300"}},
		&dns.A{Hdr: hdr("live-node.local", dns.TypeA), A: net.ParseIP("127.0.0.1").To4()},
	}
	b, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	conn.out <- mdns.QueryResponse{Msg: b}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := lv.com.Commission(ctx, commissioner.Options{
		Fabric: fabric, Passcode: testPasscode, NodeID: liveNode, Addresses: []*net.UDPAddr{nodeAddr}, OnAttestationFailure: attestation.Accept(),
	})
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	lv.peer = lv.set.Get(res.Address)
	lv.c = lv.com.Interaction(lv.peer)
	for _, ep := range lv.node.Bridge().Topology().Bridged() {
		if ep.SourceKey != nil && ep.SourceKey.String() == "plug:1" {
			lv.plugEP = ep.ID
		}
	}
	if lv.plugEP == 0 {
		t.Fatal("the plug has no endpoint")
	}
	return lv
}

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

func stringValue(v string) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: v}})
}

func (lv *live) onOff() im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: lv.plugEP, Cluster: onoff.ClusterID, Attribute: onoff.AttrOnOff, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func (lv *live) toggle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := lv.c.Invoke(ctx, im.ConcreteCommandPath{Endpoint: lv.plugEP, Cluster: onoff.ClusterID, Command: onoff.CmdToggle}, nil, interaction.InvokeOptions{}); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
}

// waitOnOff waits for a report carrying OnOff = want.
func waitOnOff(t *testing.T, reports <-chan *interaction.Report, ep uint16, want bool) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case r := <-reports:
			if v, ok := r.Value(ep, onoff.ClusterID, onoff.AttrOnOff); ok && v.El.Bool == want {
				return
			}
		case <-deadline:
			t.Fatalf("no report with OnOff = %v", want)
		}
	}
}

// TestLiveWrite: Write NodeLabel over the peer's CASE session — plain and
// timed — and read it back (ClientInteraction.ts:365-429).
func TestLiveWrite(t *testing.T) {
	t.Parallel()
	lv := newLive(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	label := rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	for i, opts := range []interaction.WriteOptions{{}, {Timed: true}} {
		want := fmt.Sprintf("written %d", i)
		res, err := lv.c.Write(ctx, []interaction.WriteRequest{{Path: label, Value: stringValue(want)}}, opts)
		if err != nil || len(res) != 1 || res.Err() != nil {
			t.Fatalf("Write %+v: %+v, %v", opts, res, err)
		}
		r, err := lv.c.Read(ctx, label)
		if err != nil {
			t.Fatal(err)
		}
		if v, ok := r.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel); !ok || v.El.String != want {
			t.Fatalf("NodeLabel %+v after writing %q", v, want)
		}
	}
	// A failing path: an unknown attribute.
	res, err := lv.c.Write(ctx, []interaction.WriteRequest{{Path: rootAttr(basicinfo.ClusterID, 0x7777), Value: stringValue("x")}}, interaction.WriteOptions{})
	var se *interaction.StatusError
	if err != nil || !errors.As(res.Err(), &se) {
		t.Fatalf("unknown attribute: %+v, %v", res, err)
	}
}

// TestLiveSubscribe: a subscription to OnOff on the bridged plug — the
// priming report, a Toggle and the pushed report through Updated (§3d), a
// keepalive, ReplaceSubscriptions closing the earlier subscription, and a
// subscription we closed answered InvalidSubscription so the node drops it.
func TestLiveSubscribe(t *testing.T) {
	t.Parallel()
	unknown := make(chan uint64, 8)
	lv := newLiveWith(t, nil, slog.New(&recordHandler{msg: "matter.controller.interaction.report_for_unknown_subscription", key: "subscription", seen: unknown}))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	reports := make(chan *interaction.Report, 16)
	keepalives := make(chan struct{}, 16)
	sub, err := lv.c.Subscribe(ctx, interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{lv.onOff()}, MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(2 * time.Second),
		Updated:           func(r *interaction.Report) { reports <- r },
		KeepaliveReceived: func() { keepalives <- struct{}{} },
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	waitOnOff(t, reports, lv.plugEP, false)
	lv.toggle(t)
	waitOnOff(t, reports, lv.plugEP, true)
	select {
	case <-keepalives:
	case <-time.After(30 * time.Second):
		t.Fatal("no keepalive")
	}

	second, err := lv.c.Subscribe(ctx, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{lv.onOff()}, ReplaceSubscriptions: true, Updated: func(r *interaction.Report) { reports <- r }})
	if err != nil {
		t.Fatalf("second Subscribe: %v", err)
	}
	select {
	case <-sub.Done():
	default:
		t.Fatal("ReplaceSubscriptions left the earlier subscription open")
	}
	// We forget the subscription; the node's next report on it is answered
	// InvalidSubscription (ClientSubscriptionHandler.ts:88-104).
	second.Close()
	lv.toggle(t)
	select {
	case id := <-unknown:
		if id != uint64(second.ID()) {
			t.Fatalf("InvalidSubscription for %d, want %d", id, second.ID())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the node's report on the closed subscription was not refused")
	}
}

// TestLiveLivenessTimeout: on the registry's fake clock a subscription
// whose deadline passes closes (ClientSubscriptions.ts:224-277).
func TestLiveLivenessTimeout(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	lv := newLive(t, clock)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sub, err := lv.c.Subscribe(ctx, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{lv.onOff()}, MaxIntervalCeiling: interaction.Interval(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	timeout := sub.MaxInterval() + 2*lv.c.MaximumPeerResponseTime(interaction.SubscriptionProcessingTime, false)
	clock.waitTimer(t, timeout)
	clock.Advance(timeout)
	select {
	case <-sub.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("no timeout")
	}
}

// TestLiveSustainedSurvivesRestart: a sustained subscription to OnOff;
// the node stops and starts again on its database; the restarted node
// re-establishes its persisted subscription by opening CASE to the
// controller (the CASE responder, §1), the session joins the peer, and the
// reports arrive through the report handler (§3d) — the re-established
// priming report, then a Toggle's.
func TestLiveSustainedSurvivesRestart(t *testing.T) {
	t.Parallel()
	lv := newLive(t, nil)
	reports := make(chan *interaction.Report, 16)
	sub, err := lv.c.Subscribe(context.Background(), interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{lv.onOff()}, Sustain: true, MaxIntervalCeiling: interaction.Interval(time.Minute),
		Updated: func(r *interaction.Report) { reports <- r },
	})
	if err != nil {
		t.Fatal(err)
	}
	waitOnOff(t, reports, lv.plugEP, false)
	deadline := time.Now().Add(10 * time.Second)
	for !sub.Active() {
		if time.Now().After(deadline) {
			t.Fatal("not active")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitPersisted(t, lv.node, 1)
	id := sub.ID()
	if err := lv.node.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	lv.node = liveNodeAt(t, lv.dbPath, func() *net.UDPAddr { return lv.addr })
	waitOnOff(t, reports, lv.plugEP, false)
	s := lv.peer.NewestSession()
	if s == nil || s.Addr().String() != lv.node.Bridge().LocalAddr() {
		t.Fatalf("the peer's newest session is not the node's inbound one: %v", s)
	}
	if sub.ID() != id || !sub.Active() {
		t.Fatalf("subscription %d active %v, want %d", sub.ID(), sub.Active(), id)
	}
	lv.toggle(t)
	waitOnOff(t, reports, lv.plugEP, true)
}

// recordHandler is a slog handler that reports the uint64 attribute key of
// every record with message msg.
type recordHandler struct {
	msg  string
	key  string
	seen chan<- uint64
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *recordHandler) WithGroup(string) slog.Handler            { return h }
func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Message != h.msg {
		return nil
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == h.key {
			select {
			case h.seen <- a.Value.Uint64():
			default:
			}
		}
		return true
	})
	return nil
}

// waitPersisted waits until the node holds want persisted subscriptions.
func waitPersisted(t *testing.T, n *node.Node, want int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		rows, err := n.Store().LoadServerSubscriptions(context.Background())
		if err == nil && len(rows) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d persisted subscriptions (%v), want %d", len(rows), err, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
