// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"log/slog"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// fakeClock is a manual exchange.Clock: timers fire, in order, inside
// Advance; waitTimers lets a test wait until goroutines armed theirs.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Time
	f       func()
	stopped bool
	fired   bool
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(2_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) exchange.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

// Advance moves the clock by d, firing every due timer in order.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	for {
		sort.SliceStable(c.timers, func(i, j int) bool { return c.timers[i].at.Before(c.timers[j].at) })
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && !t.fired && !t.at.After(target) {
				next = t
				break
			}
		}
		if next == nil {
			break
		}
		next.fired = true
		if next.at.After(c.now) {
			c.now = next.at
		}
		c.mu.Unlock()
		next.f()
		c.mu.Lock()
	}
	c.now = target
	c.mu.Unlock()
}

// eventually polls cond until it holds or ten seconds pass.
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

// datagram is one datagram the manager sent.
type datagram struct {
	to    *net.UDPAddr
	bytes []byte
}

// fakeUDP is the manager's socket: it records every datagram.
type fakeUDP struct {
	mu  sync.Mutex
	out []datagram
	ch  chan datagram
}

func newFakeUDP() *fakeUDP { return &fakeUDP{ch: make(chan datagram, 4096)} }

func (c *fakeUDP) Send(dst *net.UDPAddr, payload []byte) error {
	d := datagram{to: dst, bytes: append([]byte(nil), payload...)}
	c.mu.Lock()
	c.out = append(c.out, d)
	c.mu.Unlock()
	c.ch <- d
	return nil
}

// next returns the next datagram, failing after ten seconds.
func (c *fakeUDP) next(t *testing.T) datagram {
	t.Helper()
	select {
	case d := <-c.ch:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("no datagram sent")
		return datagram{}
	}
}

// none fails when a datagram is queued.
func (c *fakeUDP) none(t *testing.T) {
	t.Helper()
	select {
	case d := <-c.ch:
		t.Fatalf("unexpected datagram to %v", d.to)
	default:
	}
}

// drain discards the queued datagrams.
func (c *fakeUDP) drain() {
	for {
		select {
		case <-c.ch:
		default:
			return
		}
	}
}

// fakeMDNS is an mdns.QueryConn the test feeds responses into.
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

func aaaaRR(name, ip string, ttl uint32) dns.RR {
	return &dns.AAAA{Hdr: rrHdr(name, dns.TypeAAAA, ttl), AAAA: net.ParseIP(ip)}
}

// testFabric is our side of a test fabric, with no device behind it:
// controller node 112233 on fabric 0x0F0B, fabric index 1, its NOC issued
// by a controller/ca authority of its own.
func testFabric(t *testing.T) handshake.Fabric {
	t.Helper()
	const fabricID, nodeID uint64 = 0x0F0B, 112233
	authority, err := ca.New(ca.Config{})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	noc, err := authority.GenerateNOC(pub, fabricID, nodeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := authority.RootPublicKey()
	cfid, err := ca.CompressedFabricID(root, fabricID)
	if err != nil {
		t.Fatal(err)
	}
	ipk, err := sigma.DeriveOperationalIPK(make([]byte, 16), cfid)
	if err != nil {
		t.Fatal(err)
	}
	id := &sigma.Identity{
		NOC: noc, ICAC: authority.ICACCert(), PrivateKey: key,
		NodeID: nodeID, FabricID: fabricID, CompressedFabricID: cfid, IPK: ipk, FabricIndex: 1,
	}
	return handshake.Fabric{Identity: id, RootPublicKey: root, Verifier: authority.Verifier()}
}

// harness is a set on a fake socket, a fake mDNS and a fake clock.
type harness struct {
	set    *Set
	mgr    *exchange.Manager
	udp    *fakeUDP
	mdns   *fakeMDNS
	names  *discovery.Names
	clock  *fakeClock
	fabric handshake.Fabric
	store  *MemoryStore
	resume *MemoryResumptionStore
	qname  string
	hostQ  string

	mu      sync.Mutex
	seen    map[uint32]bool
	counter uint32
}

const testNode uint64 = 0x1234

var testAddr = handshake.PeerAddress{FabricIndex: 1, NodeID: testNode}

func newHarness(t *testing.T, edit func(*Context)) *harness {
	t.Helper()
	h := &harness{udp: newFakeUDP(), mdns: newFakeMDNS(), clock: newFakeClock(), fabric: testFabric(t), store: &MemoryStore{}, resume: &MemoryResumptionStore{}}
	mgr, err := exchange.NewManager(h.udp, exchange.Config{Clock: h.clock, Rand: func() float64 { return 0.5 }})
	if err != nil {
		t.Fatal(err)
	}
	h.mgr = mgr
	h.names = discovery.NewNames(h.mdns, slog.New(slog.DiscardHandler))
	c := Context{
		Exchanges:  mgr,
		Names:      h.names,
		Fabrics:    func(a handshake.PeerAddress) (handshake.Fabric, bool) { return h.fabric, a.FabricIndex == 1 },
		Resumption: h.resume,
		Store:      h.store,
		Clock:      h.clock,
		Rand:       func() float64 { return 0.5 },
	}
	if edit != nil {
		edit(&c)
	}
	set, err := NewSet(c)
	if err != nil {
		t.Fatal(err)
	}
	h.set = set
	h.qname = discovery.OperationalQName(h.fabric.Identity.CompressedFabricID, testNode)
	h.hostQ = "node1234.local"
	t.Cleanup(func() {
		set.Close(closingCtx())
		mgr.Close()
		_ = h.names.Close()
	})
	return h
}

// secureSession registers a CASE-like session with the test node.
func (h *harness) secureSession(t *testing.T, id uint16, addr *net.UDPAddr) *exchange.Session {
	t.Helper()
	s, _ := h.securePair(t, id, addr)
	return s
}

// securePair registers a CASE-like session with the test node and
// returns the node's side of its keys.
func (h *harness) securePair(t *testing.T, id uint16, addr *net.UDPAddr) (s *exchange.Session, node *channel.Session) {
	t.Helper()
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	s, err := h.mgr.NewSecureSession(exchange.SecureSessionConfig{
		ID:             id,
		Addr:           addr,
		Channel:        channel.Config{EncryptKey: key, DecryptKey: key, LocalNodeID: 1, PeerNodeID: testNode, PeerSessionID: id},
		PeerParameters: exchange.PeerParameters(nil),
		FabricIndex:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err = channel.New(channel.Config{EncryptKey: key, DecryptKey: key, LocalNodeID: testNode, PeerNodeID: 1, PeerSessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	return s, node
}

func udpAddr(s string) *net.UDPAddr {
	a, err := net.ResolveUDPAddr("udp", s)
	if err != nil {
		panic(err)
	}
	return a
}

// closingCtx is the context of a Close in a cleanup.
func closingCtx() context.Context { return context.Background() }
