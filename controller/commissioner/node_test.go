// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/udp"
)

const (
	nodePasscode      uint32 = 20202021
	nodeDiscriminator uint16 = 3840
	testFabricID      uint64 = 0x0000_0000_0000_0F0B
	controllerNodeID  uint64 = 112233
	commissionedNode  uint64 = 0x0000_0000_0000_4321
	fabricLabel              = "go-fabric commissioner"
)

// startNode starts an uncommissioned node on loopback, as the
// client/handshake and client/peer tests do.
func startNode(t *testing.T, adv mdns.Advertiser, listen string) (*node.Node, *net.UDPAddr) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "node.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := node.New(node.Config{
		Network:            node.Network{Listen: listen, PreferIPv4: listen == "127.0.0.1:0"},
		Advertiser:         adv,
		Commissioning:      node.Commissioning{Passcode: nodePasscode, Discriminator: nodeDiscriminator, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "commissioner test", NodeLabel: "commissioned node", SerialNumber: "COM-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:  node.Store{DB: db},
		Logger: discard(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	return n, addr
}

// fakeMDNS is the DNS-SD socket of the cache: answers are injected.
type fakeMDNS struct{ out chan mdns.QueryResponse }

func newFakeMDNS() *fakeMDNS                             { return &fakeMDNS{out: make(chan mdns.QueryResponse, 256)} }
func (f *fakeMDNS) Send([]byte) error                    { return nil }
func (f *fakeMDNS) Responses() <-chan mdns.QueryResponse { return f.out }
func (f *fakeMDNS) Close() error                         { return nil }

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

// controller is the commissioner side of a test: a socket, the DNS-SD
// cache, the CA and fabric, the peer set and the commissioner.
type controller struct {
	mgr     *exchange.Manager
	names   *discovery.Names
	scanner *discovery.Scanner
	set     *peer.Set
	fabric  *Fabric
	com     *Commissioner
}

func newController(t *testing.T, names *discovery.Names, listen string) *controller {
	t.Helper()
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := newControllerWith(t, names, listen, authority, FabricConfig{FabricID: testFabricID, RootNodeID: controllerNodeID, VendorID: 0xFFF1, Label: fabricLabel})
	return c
}

// newControllerWith builds a controller over a given CA and fabric
// configuration; the returned func tears it down (once; the test's
// cleanup calls it too).
func newControllerWith(t *testing.T, names *discovery.Names, listen string, authority *ca.CA, fcfg FabricConfig) (c *controller, closeAll func()) {
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
	fabric, err := NewFabric(authority, fcfg)
	if err != nil {
		t.Fatal(err)
	}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: &peer.MemoryResumptionStore{}, Logger: discard()})
	if err != nil {
		t.Fatal(err)
	}
	com, err := New(Context{Exchanges: mgr, Peers: set, Attestation: attestation.TestStore(), Logger: discard()})
	if err != nil {
		t.Fatal(err)
	}
	c = &controller{mgr: mgr, names: names, scanner: discovery.NewScanner(names), set: set, fabric: fabric, com: com}
	var once sync.Once
	closeAll = func() {
		once.Do(func() {
			c.scanner.Close()
			com.Close()
			set.Close(context.Background())
			mgr.Close()
			cancel()
			<-served
			_ = names.Close()
		})
	}
	t.Cleanup(closeAll)
	return c, closeAll
}

// announceNode injects the node's commissionable record and its
// operational record for nodeID on the controller's fabric.
func (c *controller) announceNode(t *testing.T, conn *fakeMDNS, port int, nodeID uint64) {
	t.Helper()
	const inst = "C0FFEE0123456789._matterc._udp.local"
	const host = "commissioned-node.local"
	cfid := c.fabric.Identity.CompressedFabricID
	op := discovery.OperationalQName(cfid, nodeID)
	conn.announce(t,
		&dns.PTR{Hdr: dns.RR_Header{Name: "_matterc._udp.local.", Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 4500}, Ptr: inst + "."},
		&dns.SRV{Hdr: rrHdr(inst, dns.TypeSRV, 120), Target: host + ".", Port: uint16(port)}, //nolint:gosec // a test port
		&dns.TXT{Hdr: rrHdr(inst, dns.TypeTXT, 4500), Txt: []string{"D=3840", "CM=1", "VP=65521+32769", "SII=500", "SAI=300"}},
		&dns.SRV{Hdr: rrHdr(op, dns.TypeSRV, 120), Target: host + ".", Port: uint16(port)}, //nolint:gosec // a test port
		&dns.TXT{Hdr: rrHdr(op, dns.TypeTXT, 4500), Txt: []string{"SII=500", "SAI=300"}},
		&dns.A{Hdr: rrHdr(host, dns.TypeA, 120), A: net.ParseIP("127.0.0.1").To4()},
	)
}

// find runs the scanner until it reports the node.
func (c *controller) find(t *testing.T) discovery.Device {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var found *discovery.Device
	_, err := c.scanner.FindCommissionable(ctx, discovery.ByLongDiscriminator(nodeDiscriminator), func(d discovery.Device) {
		found = &d
		cancel()
	})
	if err != nil || found == nil {
		t.Fatalf("FindCommissionable: %v (found %v)", err, found != nil)
	}
	return *found
}

func udpAddrs(d discovery.Device) []*net.UDPAddr {
	out := make([]*net.UDPAddr, 0, len(d.Addresses))
	for _, a := range d.Addresses {
		out = append(out, a.UDPAddr())
	}
	return out
}

// TestCommissionTheNode: discovery → Commission → the node's fabric table
// holds the fabric with our label, the CASE session is the peer's,
// CommissioningComplete closed the failsafe, a Read over CASE works; a
// second commissioning into the same fabric is refused by AddNOC
// (FabricConflict) and disarms the failsafe; a wrong passcode drops the
// device; FinalizeCommissioning stops the flow before
// CommissioningComplete.
func TestCommissionTheNode(t *testing.T) {
	t.Parallel()
	n, addr := startNode(t, nil, "127.0.0.1:0")
	conn := newFakeMDNS()
	c := newController(t, discovery.NewNames(conn, discard()), "127.0.0.1:0")
	c.announceNode(t, conn, addr.Port, commissionedNode)
	dev := c.find(t)
	if dev.D != nodeDiscriminator || len(dev.Addresses) == 0 {
		t.Fatalf("device %+v", dev)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// A wrong passcode: the device is dropped; the error is the passcode's.
	_, err := c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: 11111111, NodeID: commissionedNode, Addresses: udpAddrs(dev), DiscoveryData: &dev})
	if !errors.Is(err, handshake.ErrPasscodeMismatch) {
		t.Fatalf("wrong passcode: %v", err)
	}

	res, err := c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: nodePasscode, NodeID: commissionedNode, Addresses: udpAddrs(dev), DiscoveryData: &dev})
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	if res.Address != c.fabric.AddressOf(commissionedNode) || res.FabricIndexOnPeer == 0 {
		t.Fatalf("result %+v", res)
	}
	fabrics, err := n.FabricInfos(ctx)
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("node fabrics %+v (%v)", fabrics, err)
	}
	if f := fabrics[0]; f.FabricID != testFabricID || f.NodeID != commissionedNode || f.Label != fabricLabel || f.VendorID != 0xFFF1 {
		t.Fatalf("node fabric %+v", f)
	}
	if n.Root().GeneralCommissioning.FailSafeArmed() {
		t.Fatal("the failsafe is still armed after CommissioningComplete")
	}
	p := c.set.Get(res.Address)
	s := p.NewestSession()
	if p == nil || s == nil || s.PeerNodeID() != commissionedNode || s.NodeID() != controllerNodeID {
		t.Fatal("the CASE session is not the peer's")
	}
	report, err := interaction.New(c.mgr, s, nil).Read(ctx, rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel))
	if err != nil {
		t.Fatalf("Read over CASE: %v", err)
	}
	if v, ok := report.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel); !ok {
		t.Fatal("no NodeLabel")
	} else if label, _ := spec.DecodeString(v); label != "commissioned node" {
		t.Fatalf("NodeLabel %q", label)
	}

	// A second commissioning into the same fabric: AddNOC answers
	// FabricConflict, the flow disarms the failsafe, the peer is deleted.
	if err := n.OpenBasicWindow(ctx, node.WindowCredentials{Discriminator: nodeDiscriminator, Passcode: nodePasscode, Iterations: 1000, Salt: []byte("SPAKE2P Key Salt")}); err != nil {
		t.Fatalf("OpenBasicWindow: %v", err)
	}
	_, err = c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: nodePasscode, Addresses: []*net.UDPAddr{addr}})
	if !errors.Is(err, ErrDeviceAlreadyCommissionedToThisFabric) {
		t.Fatalf("second commissioning: %v", err)
	}
	if n.Root().GeneralCommissioning.FailSafeArmed() {
		t.Fatal("the failsafe was not disarmed")
	}
	if c.set.Size() != 1 {
		t.Fatalf("%d peers after the refused commissioning, want 1", c.set.Size())
	}
	// The requested node id is a peer now.
	if _, err := c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: nodePasscode, NodeID: commissionedNode, Addresses: []*net.UDPAddr{addr}}); !errors.Is(err, ErrNodeIDConflict) {
		t.Fatalf("node id conflict: %v", err)
	}
}

// TestFinalizeCommissioning: FinalizeCommissioning replaces the CASE
// connection and the flow stops there (CC:687-690, CF:2035-2041): no
// CommissioningComplete, so the failsafe stays armed; the parameters the
// PASE session reported are handed over.
func TestFinalizeCommissioning(t *testing.T) {
	t.Parallel()
	n, addr := startNode(t, nil, "127.0.0.1:0")
	c := newController(t, discovery.NewNames(newFakeMDNS(), discard()), "127.0.0.1:0")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var finalized handshake.PeerAddress
	var params *sessionparams.Parameters
	res, err := c.com.Commission(ctx, Options{
		Fabric: c.fabric, Passcode: nodePasscode, Addresses: []*net.UDPAddr{addr},
		OnAttestationFailure: attestation.Accept(),
		FinalizeCommissioning: func(_ context.Context, a handshake.PeerAddress, _ *discovery.Device, p *sessionparams.Parameters) error {
			finalized, params = a, p
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	if finalized != res.Address || finalized.NodeID == 0 || params == nil || res.FabricIndexOnPeer == 0 {
		t.Fatalf("finalized %+v, params %v, result %+v", finalized, params, res)
	}
	if !n.Root().GeneralCommissioning.FailSafeArmed() {
		t.Fatal("the flow went past the finalization")
	}
	if c.set.Size() != 0 {
		t.Fatal("the built-in CASE connection ran")
	}
}

// TestRejectedAttestation: a policy that rejects the findings ends the
// commissioning with a CommissioningError and disarms the failsafe.
func TestRejectedAttestation(t *testing.T) {
	t.Parallel()
	n, addr := startNode(t, nil, "127.0.0.1:0")
	c := newController(t, discovery.NewNames(newFakeMDNS(), discard()), "127.0.0.1:0")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: nodePasscode, Addresses: []*net.UDPAddr{addr}, OnAttestationFailure: attestation.Reject()})
	if !errors.Is(err, ErrCommissioning) || err.Error() != "commissioner: Device attestation produced 1 finding(s) and was rejected by policy" {
		t.Fatalf("rejected attestation: %v", err)
	}
	if n.Root().GeneralCommissioning.FailSafeArmed() {
		t.Fatal("the failsafe was not disarmed")
	}
	// matter.js closes the PASE session without CloseSession (CC:313-319),
	// so the next case runs against a node of its own.
	_, other := startNode(t, nil, "127.0.0.1:0")
	_, err = c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: nodePasscode, Addresses: []*net.UDPAddr{other}, OnAttestationFailure: attestation.Policy{
		Decide: func(f []attestation.Finding) (bool, string, error) { return false, "not this one", nil },
	}})
	if !errors.Is(err, ErrCommissioning) || err.Error() != "commissioner: not this one: commissioner: Device attestation produced 1 finding(s) and was rejected by policy" {
		t.Fatalf("rejected with a reason: %v", err)
	}
}

// TestEstablishPASE: a session without the flow; ContinueAfterPASE false
// closes it.
func TestEstablishPASE(t *testing.T) {
	t.Parallel()
	_, addr := startNode(t, nil, "127.0.0.1:0")
	c := newController(t, discovery.NewNames(newFakeMDNS(), discard()), "127.0.0.1:0")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, _, err := c.com.EstablishPASE(ctx, PASEOptions{Addresses: []*net.UDPAddr{addr}, Passcode: nodePasscode})
	if err != nil || s == nil || len(s.AttestationChallenge()) != 16 {
		t.Fatalf("EstablishPASE: %v", err)
	}
	// The device answers no second PBKDFParamRequest while a PASE session
	// of its window lives (bridge: matter.rx.sc.pase_busy), so the next
	// case runs against a node of its own.
	c.mgr.CloseSession(ctx, s)
	_, other := startNode(t, nil, "127.0.0.1:0")
	_, _, err = c.com.EstablishPASE(ctx, PASEOptions{Addresses: []*net.UDPAddr{other}, Passcode: nodePasscode, ContinueAfterPASE: func() bool { return false }})
	if !errors.Is(err, ErrCommissioning) {
		t.Fatalf("ContinueAfterPASE false: %v (%v)", err, errors.Unwrap(err))
	}
	// Nothing answers here: PeerCommunicationError (CC:402-407).
	dead := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9}
	_, _, err = c.com.EstablishPASE(ctx, PASEOptions{Addresses: []*net.UDPAddr{dead}, Passcode: nodePasscode, Timeout: 2 * time.Second})
	var pce *PeerCommunicationError
	if !errors.As(err, &pce) || pce.Message != "Could not connect to device" {
		t.Fatalf("unreachable: %v", err)
	}
}

// TestAcceptedFailedAttestation: a hard attestation failure the policy
// accepts goes on with the DAC key read from the DAC itself (CF:1321-1331);
// the CSR signature is still checked against it.
func TestAcceptedFailedAttestation(t *testing.T) {
	t.Parallel()
	_, addr := startNode(t, nil, "127.0.0.1:0")
	c := newController(t, discovery.NewNames(newFakeMDNS(), discard()), "127.0.0.1:0")
	c.com.ctx.Attestation = &attestation.StaticStore{AcceptTest: true} // no PAA: PaaNotTrusted
	var seen []attestation.Finding
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := c.com.Commission(ctx, Options{
		Fabric: c.fabric, Passcode: nodePasscode, Addresses: []*net.UDPAddr{addr},
		OnAttestationFailure: attestation.Policy{Decide: func(f []attestation.Finding) (bool, string, error) {
			seen = f
			return true, "", nil
		}},
		FinalizeCommissioning: func(context.Context, handshake.PeerAddress, *discovery.Device, *sessionparams.Parameters) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	if len(seen) != 1 || seen[0].Check != attestation.PaaNotTrusted || seen[0].Level != attestation.LevelError {
		t.Fatalf("findings %+v", seen)
	}
	if _, err := dacKeyOf([]byte{1, 2}); err == nil {
		t.Fatal("a garbage DAC parsed")
	}
}

// forgetRecorder records the scanners' ForgetCommissioned calls.
type forgetRecorder struct {
	mu    sync.Mutex
	calls []*discovery.CommissionedIdentity
	addrs [][]string
}

func (r *forgetRecorder) ForgetCommissioned(addresses []string, identity *discovery.CommissionedIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, identity)
	r.addrs = append(r.addrs, addresses)
}

// TestCommissionForgetsTheDevice: once commissioned, every scanner forgets
// the device by its identity — D and VP of its discovery data — and no
// BLE address, the device having been reached over UDP; a failed
// commissioning forgets nothing (ControllerCommissioner.ts:321-341,
// :721-723 at 862ad75f).
func TestCommissionForgetsTheDevice(t *testing.T) {
	t.Parallel()
	_, addr := startNode(t, nil, "127.0.0.1:0")
	conn := newFakeMDNS()
	c := newController(t, discovery.NewNames(conn, discard()), "127.0.0.1:0")
	rec := &forgetRecorder{}
	c.com.ctx.Scanners = []CommissionedScanner{rec}
	c.announceNode(t, conn, addr.Port, commissionedNode)
	dev := c.find(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: 11111111, NodeID: commissionedNode, Addresses: udpAddrs(dev), DiscoveryData: &dev}); err == nil {
		t.Fatal("a wrong passcode commissioned")
	}
	rec.mu.Lock()
	if len(rec.calls) != 0 {
		t.Errorf("a failed commissioning forgot the device: %+v", rec.calls)
	}
	rec.mu.Unlock()
	if _, err := c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: nodePasscode, NodeID: commissionedNode, Addresses: udpAddrs(dev), DiscoveryData: &dev}); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) != 1 || rec.calls[0] == nil || *rec.calls[0] != (discovery.CommissionedIdentity{D: nodeDiscriminator, VP: "65521+32769"}) || len(rec.addrs[0]) != 0 {
		t.Fatalf("forgot %+v at %v, want the device's identity once and no BLE address", rec.calls, rec.addrs)
	}
}
