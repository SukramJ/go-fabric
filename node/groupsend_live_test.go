// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node_test

// The group sender live: two node.Nodes in one fabric, commissioned by the
// module's own commissioner (the harness of controller/client/live_test.go),
// provisioned with one key set and joined to one group — the receiver with
// its OnOff plug, the sender sender-only — and a group Toggle from the
// sender flips the receiver's plug.

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/udp"
)

const (
	groupLiveFabricID   uint64 = 0x0000_0000_0000_0F5D
	groupLiveController uint64 = 112233
	groupLivePasscode   uint32 = 20202021
	groupLiveGroup      uint16 = 0x0101
	groupLiveKeySet     uint16 = 0x01A1
)

var groupLiveEpochKey = []byte{0xD0, 0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0xDA, 0xDB, 0xDC, 0xDD, 0xDE, 0xDF}

// groupPlug is a bridged OnOffPlugInUnit.
type groupPlug struct {
	key string
	srv *onoff.Server
}

func (p *groupPlug) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey(p.key), DeviceAddress: p.key, ChannelAddress: p.key + ":0",
		DeviceType: onoff.DeviceTypeOnOffPlugInUnit, FriendlyName: p.key, Source: p,
	}
}
func (p *groupPlug) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffPlugInUnit }
func (p *groupPlug) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p.srv}
}

// groupLiveMDNS sends nowhere and receives what the test injects.
type groupLiveMDNS struct{ out chan mdns.QueryResponse }

func (f *groupLiveMDNS) Send([]byte) error                    { return nil }
func (f *groupLiveMDNS) Responses() <-chan mdns.QueryResponse { return f.out }
func (f *groupLiveMDNS) Close() error                         { return nil }

// encodable writes a payload through a function.
type encodable func(enc *tlv.Encoder, tag tlv.Tag)

func (f encodable) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { f(enc, tag) }

// unicastRedirect is a groups.MulticastTransport that records the
// multicast destination it was asked for and delivers the datagram to the
// receiver's unicast address — the bridge takes a group message on any
// datagram (bridge/receive.go:91-97, isGroupDatagram in
// bridge/group_messaging.go:237-239 tests only the Security Flags), so the
// frame reaches the receive path without the host's multicast routing and
// without the receiver on port 5540.
type unicastRedirect struct {
	l   *udp.Listener
	dst *net.UDPAddr

	mu    sync.Mutex
	group net.IP
	port  int
}

func (r *unicastRedirect) SendMulticast(group net.IP, port int, payload []byte) error {
	r.mu.Lock()
	r.group, r.port = group, port
	r.mu.Unlock()
	return r.l.Send(r.dst, payload)
}

func groupLiveNode(t *testing.T, name string) (*node.Node, *onoff.Server) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), name+".db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := node.New(node.Config{
		Network:            node.Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Commissioning:      node.Commissioning{Passcode: groupLivePasscode, Discriminator: 3840, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "group live test", NodeLabel: name, SerialNumber: name,
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:      node.Store{DB: db},
		Advertiser: mdns.NewNoop(),
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := onoff.New(onoff.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(context.Background(), &groupPlug{key: name + ":plug", srv: srv}); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n, srv
}

// TestLiveGroupSend: the sender's Groups().Sender(fabric, group).Invoke of
// OnOff Toggle — a group InvokeRequest with SuppressResponse, sealed under
// the operational group key the controller provisioned, privacy-obfuscated,
// from the sender's node id — flips the receiver's plug, through the
// receiver's own group receive path (Decode, the auxiliary ACL Groupcast
// grants, the group table's endpoint).
func TestLiveGroupSend(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	log := slog.New(slog.DiscardHandler)

	receiver, plug := groupLiveNode(t, "receiver")
	sender, _ := groupLiveNode(t, "sender")

	// The controller: exchange manager, commissioner and one fabric.
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := exchange.NewManager(l, exchange.Config{Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, stopServe := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(serveCtx, mgr.HandleDatagram)
	}()
	conn := &groupLiveMDNS{out: make(chan mdns.QueryResponse, 64)}
	names := discovery.NewNames(conn, log)
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: groupLiveFabricID, RootNodeID: groupLiveController, VendorID: 0xFFF1, Label: "groups"})
	if err != nil {
		t.Fatal(err)
	}
	resumption := &peer.MemoryResumptionStore{}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: resumption, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: log})
	com, err := commissioner.New(commissioner.Context{
		Exchanges: mgr, Peers: set, Attestation: attestation.TestStore(), Logger: log,
		Subscriptions: reg, Fabrics: []*commissioner.Fabric{fabric}, Resumption: resumption,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reg.Close(context.Background())
		com.Close()
		set.Close(context.Background())
		mgr.Close()
		stopServe()
		<-served
		_ = names.Close()
	})

	// Commission both nodes into the fabric.
	commission := func(n *node.Node, nodeID uint64, host string) *interaction.Interaction {
		t.Helper()
		addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
		if err != nil {
			t.Fatal(err)
		}
		op := discovery.OperationalQName(fabric.Identity.CompressedFabricID, nodeID)
		hdr := func(name string, typ uint16) dns.RR_Header {
			return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: typ, Class: dns.ClassINET | 0x8000, Ttl: 120}
		}
		msg := new(dns.Msg)
		msg.Response = true
		msg.Answer = []dns.RR{
			&dns.SRV{Hdr: hdr(op, dns.TypeSRV), Target: host + ".local.", Port: uint16(addr.Port)}, //nolint:gosec // a test port
			&dns.TXT{Hdr: hdr(op, dns.TypeTXT), Txt: []string{"SII=500", "SAI=300"}},
			&dns.A{Hdr: hdr(host+".local", dns.TypeA), A: net.ParseIP("127.0.0.1").To4()},
		}
		b, err := msg.Pack()
		if err != nil {
			t.Fatal(err)
		}
		conn.out <- mdns.QueryResponse{Msg: b}
		res, err := com.Commission(ctx, commissioner.Options{
			Fabric: fabric, Passcode: groupLivePasscode, NodeID: nodeID, Addresses: []*net.UDPAddr{addr}, OnAttestationFailure: attestation.Accept(),
		})
		if err != nil {
			t.Fatalf("Commission %s: %v", host, err)
		}
		return com.Interaction(set.Get(res.Address))
	}
	rxCtl := commission(receiver, 0x4321, "receiver")
	txCtl := commission(sender, 0x4322, "sender")

	plugEP := uint16(0)
	for _, ep := range receiver.Bridge().Topology().Bridged() {
		if ep.SourceKey != nil && ep.SourceKey.String() == "receiver:plug" {
			plugEP = ep.ID
		}
	}
	if plugEP == 0 {
		t.Fatal("the receiver's plug has no endpoint")
	}

	// The same key set on both (GroupKeyManagement KeySetWrite, §11.2.7.1),
	// then Groupcast JoinGroup, which binds the group to it in GroupKeyMap:
	// the receiver with its plug and the auxiliary ACL entry that grants
	// the group Operate on it, the sender with no endpoint (sender-only).
	keySetWrite := encodable(func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.StartStruct(tlv.ContextTag(0))
		enc.PutUint(tlv.ContextTag(0), uint64(groupLiveKeySet))
		enc.PutUint(tlv.ContextTag(1), 0) // TrustFirst
		enc.PutOctets(tlv.ContextTag(2), groupLiveEpochKey)
		enc.PutUint(tlv.ContextTag(3), 1_110_000)
		for i := uint8(4); i <= 7; i++ {
			enc.PutNull(tlv.ContextTag(i))
		}
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	})
	joinGroup := func(endpoints []uint16, aux bool) encodable {
		return func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.StartStruct(tag)
			enc.PutUint(tlv.ContextTag(0), uint64(groupLiveGroup))
			enc.StartArray(tlv.ContextTag(1))
			for _, ep := range endpoints {
				enc.PutUint(tlv.AnonymousTag(), uint64(ep))
			}
			_ = enc.EndContainer()
			enc.PutUint(tlv.ContextTag(2), uint64(groupLiveKeySet))
			if aux {
				enc.PutBool(tlv.ContextTag(4), true)
			}
			_ = enc.EndContainer()
		}
	}
	for name, step := range map[string]struct {
		c       *interaction.Interaction
		cluster uint32
		fields  encodable
	}{
		"receiver KeySetWrite": {rxCtl, 0x003F, keySetWrite},
		"sender KeySetWrite":   {txCtl, 0x003F, keySetWrite},
	} {
		if _, err := step.c.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: step.cluster, Command: 0x00}, step.fields, interaction.InvokeOptions{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := rxCtl.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0065, Command: 0x00}, joinGroup([]uint16{plugEP}, true), interaction.InvokeOptions{}); err != nil {
		t.Fatalf("receiver JoinGroup: %v", err)
	}
	if _, err := txCtl.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0065, Command: 0x00}, joinGroup(nil, false), interaction.InvokeOptions{}); err != nil {
		t.Fatalf("sender JoinGroup (sender-only): %v", err)
	}

	fabrics, err := sender.Fabrics(ctx)
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("sender fabrics: %v, %v", fabrics, err)
	}
	ms, err := sender.Groups().GroupcastMemberships(ctx)
	if err != nil || len(ms) != 1 || ms[0].GroupID != groupLiveGroup || len(ms[0].Endpoints) != 0 || ms[0].KeySetID != groupLiveKeySet {
		t.Fatalf("sender membership: %+v, %v", ms, err)
	}

	// The frame goes to the receiver's unicast address (see
	// unicastRedirect); transport/udp's TestSendMulticastReachesAJoinedListener
	// proves the multicast leg.
	rxAddr, err := net.ResolveUDPAddr("udp", receiver.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	txSock, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = txSock.Close() })
	redirect := &unicastRedirect{l: txSock, dst: rxAddr}
	sender.Groups().SetTransport(redirect)

	s, err := sender.Groups().Sender(ctx, fabrics[0].FabricIndex, groupLiveGroup)
	if err != nil {
		t.Fatalf("Sender: %v", err)
	}
	if s.NodeID() != 0x4322 || s.KeySetID() != groupLiveKeySet || !s.Address().Equal(groups.IANAGroupcastAddress) {
		t.Fatalf("sender session: node %#x key set %#x address %s", s.NodeID(), s.KeySetID(), s.Address())
	}
	toggle := im.ConcreteCommandPath{Cluster: 0x0006, Command: 0x02}
	for i, want := range []bool{true, false} {
		if err := s.Invoke(ctx, toggle, nil, groups.RequestOptions{}); err != nil {
			t.Fatalf("Toggle %d: %v", i, err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for plug.OnOff() != want {
			if time.Now().After(deadline) {
				t.Fatalf("Toggle %d: the receiver's plug is %v, want %v", i, plug.OnOff(), want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	redirect.mu.Lock()
	group, port := redirect.group, redirect.port
	redirect.mu.Unlock()
	if !group.Equal(groups.IANAGroupcastAddress) || port != 5540 {
		t.Errorf("sent to [%s]:%d, want [ff05::fa]:5540 (IanaAddr policy, the standard port)", group, port)
	}
	if err := s.Invoke(ctx, toggle, nil, groups.RequestOptions{Timed: true}); err == nil {
		t.Error("a timed group invoke went out")
	}
}
