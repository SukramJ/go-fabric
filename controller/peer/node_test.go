// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer_test

import (
	"context"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// The node proofs commission a node.Node through bridge/bridgetest, which
// imports this package: they live in the external test package (brief AJ
// §5), with their own copies of the DNS-SD helpers of helpers_test.go.

const (
	nodePasscode uint32 = 20202021
	nodeFabricID uint64 = 0x0000_0000_0000_0F0B
	testNode     uint64 = 0x1234
)

var testAddr = handshake.PeerAddress{FabricIndex: 1, NodeID: testNode}

// fakeMDNS is an mDNS socket that sends nowhere; announce injects responses.
type fakeMDNS struct{ out chan mdns.QueryResponse }

func newFakeMDNS() *fakeMDNS                             { return &fakeMDNS{out: make(chan mdns.QueryResponse, 256)} }
func (f *fakeMDNS) Send([]byte) error                    { return nil }
func (f *fakeMDNS) Responses() <-chan mdns.QueryResponse { return f.out }
func (f *fakeMDNS) Close() error                         { return nil }

// announce injects one response with rrs.
func (f *fakeMDNS) announce(t *testing.T, rrs ...dns.RR) {
	t.Helper()
	m := new(dns.Msg)
	m.Response = true
	m.Answer = rrs
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	f.out <- mdns.QueryResponse{Msg: b}
}

func rrHdr(name string, typ uint16, ttl uint32) dns.RR_Header {
	return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: typ, Class: dns.ClassINET | 0x8000, Ttl: ttl}
}

func srvRR(name, target string, port uint16, ttl uint32) dns.RR {
	return &dns.SRV{Hdr: rrHdr(name, dns.TypeSRV, ttl), Target: dns.Fqdn(target), Port: port}
}

func txtRR(name string, ttl uint32, entries ...string) dns.RR {
	return &dns.TXT{Hdr: rrHdr(name, dns.TypeTXT, ttl), Txt: entries}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// startNode starts an uncommissioned node with the system clock on a
// socket listening on listen — a fabrictest device on loopback, as these
// tests drive a peer set of their own on a socket — advertising with adv
// (nil: nothing).
func startNode(t *testing.T, adv mdns.Advertiser, listen string) (*node.Node, *net.UDPAddr) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true, Clock: clock.System{}})
	d := site.AddDevice(fabrictest.Topology{Configure: func(c *node.Config) {
		c.Network = node.Network{Listen: listen, PreferIPv4: listen == "127.0.0.1:0"}
		if adv != nil {
			c.Advertiser = adv
		}
		c.Commissioning.Passcode = nodePasscode
		c.BasicInformation = node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "peer test", NodeLabel: "peer node", SerialNumber: "PEER-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		}
	}})
	return d.Node(), d.Addr()
}

// commissioned starts a node, commissions it with the test commissioner
// and returns it, its address and our side of the fabric as controller
// node 112233 on fabric index 1.
func commissioned(t *testing.T, adv mdns.Advertiser, listen string) (*node.Node, *net.UDPAddr, handshake.Fabric) {
	t.Helper()
	n, addr := startNode(t, adv, listen)
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), bridgetest.Options{
		Passcode: nodePasscode, Discriminator: 3840, FabricID: nodeFabricID, NodeID: testNode, Seed: 7,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := c.Commission(ctx)
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	s.Close()
	id := c.ControllerIdentity(bridgetest.DefaultControllerNodeID)
	id.FabricIndex = 1
	v, err := mattercert.NewVerifier(c.RootPublicKey(), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	return n, addr, handshake.Fabric{Identity: &id, RootPublicKey: c.RootPublicKey(), Verifier: v}
}

// liveSet is a set on a real loopback socket and the system clock.
type liveSet struct {
	set    *peer.Set
	mgr    *exchange.Manager
	resume *peer.MemoryResumptionStore
	names  *discovery.Names
}

func newLiveSet(t *testing.T, names *discovery.Names, fabric handshake.Fabric, listen string, edit func(*peer.Context)) *liveSet {
	t.Helper()
	l, err := udp.New(udp.Config{LocalAddr: listen, PreferIPv4: listen == "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := exchange.NewManager(l, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(ctx, mgr.HandleDatagram)
	}()
	ls := &liveSet{mgr: mgr, resume: &peer.MemoryResumptionStore{}, names: names}
	c := peer.Context{
		Exchanges:  mgr,
		Names:      names,
		Fabrics:    func(a handshake.PeerAddress) (handshake.Fabric, bool) { return fabric, a.FabricIndex == 1 },
		Resumption: ls.resume,
		Logger:     slog.New(slog.DiscardHandler),
	}
	if edit != nil {
		edit(&c)
	}
	ls.set, err = peer.NewSet(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ls.set.Close(context.Background())
		mgr.Close()
		cancel()
		<-served
		_ = names.Close()
	})
	return ls
}

func aRR(name, ip string, ttl uint32) dns.RR {
	return &dns.A{Hdr: rrHdr(name, dns.TypeA, ttl), A: net.ParseIP(ip).To4()}
}

// TestConnectToANodeThroughTheProcess: the process finds the node through
// its operational record — injected here, so the path runs under -race —
// and establishes CASE; a second Connect reuses the session; the node's
// CloseSession on stop drops the session and fires Disconnected; Delete
// forgets the resumption record.
func TestConnectToANodeThroughTheProcess(t *testing.T) {
	t.Parallel()
	n, addr, fabric := commissioned(t, nil, "127.0.0.1:0")
	conn := newFakeMDNS()
	names := discovery.NewNames(conn, slog.New(slog.DiscardHandler))
	var disconnected atomic.Int32
	ls := newLiveSet(t, names, fabric, "127.0.0.1:0", func(c *peer.Context) { c.Disconnected = func(*peer.Peer) { disconnected.Add(1) } })
	p, err := ls.set.For(testAddr)
	if err != nil {
		t.Fatal(err)
	}
	qname := discovery.OperationalQName(fabric.Identity.CompressedFabricID, testNode)
	conn.announce(t, srvRR(qname, "peer-node.local", uint16(addr.Port), 120), txtRR(qname, 120, "SII=500", "SAI=300"), aRR("peer-node.local", "127.0.0.1", 120))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := p.Connect(ctx, peer.ConnectOptions{})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if s.PeerNodeID() != testNode || s.FabricIndex() != 1 || p.Descriptor().OperationalAddress.String() != addr.String() {
		t.Fatalf("session to 0x%X on fabric %d, operational address %v", s.PeerNodeID(), s.FabricIndex(), p.Descriptor().OperationalAddress)
	}
	if again, err := p.Connect(ctx, peer.ConnectOptions{}); err != nil || again != s {
		t.Fatal("a second Connect did not reuse the session")
	}
	if !p.Service().Reachable() {
		t.Fatal("a connected service is not reachable")
	}
	if _, ok := ls.resume.FindResumptionRecord(testAddr); !ok {
		t.Fatal("CASE saved no resumption record")
	}
	// The node tells a session it has routed a message of (bridge.go's
	// notify path); the probe gives it the route, and proves the session.
	if !peer.Probe(ctx, ls.mgr, s) {
		t.Fatal("the probe failed on a live session")
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the node's CloseSession", func() bool { return s.IsPeerLost() && !p.HasSession() && disconnected.Load() == 1 })
	if peer.Probe(ctx, ls.mgr, s) {
		t.Fatal("the probe succeeded on a closed session")
	}
	p.Delete(ctx)
	if _, ok := ls.resume.FindResumptionRecord(testAddr); ok {
		t.Fatal("Delete kept the resumption record")
	}
}

// readOnce reads BasicInformation.NodeLabel over s.
func readOnce(t *testing.T, m *exchange.Manager, s *exchange.Session) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	x, err := m.Initiate(s, im.InteractionModelProtocolID, exchange.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	enc := tlv.NewEncoder()
	im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{{
		Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005, HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}}, FabricFiltered: true}.MarshalTLV(enc)
	req, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Send(ctx, im.OpcodeReadRequest, req, exchange.SendOptions{}); err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	if msg, err := x.Next(ctx, exchange.ReceiveOptions{}); err != nil || msg.Opcode != im.OpcodeReportData {
		t.Fatalf("ReportData: %v", err)
	}
}
