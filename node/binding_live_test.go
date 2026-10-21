// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/binding"
	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// The node ids of TC-BIND-2.1 (Test_TC_BIND_2_1.yaml at the harness pin
// 6170af84: the DUT is node 1, TH2 node 2, TH3 node 3, TH1 the commissioner
// 112233).
const (
	bindFabricID   uint64 = 0x0000_0000_0000_0B1D
	bindController uint64 = 112233
	bindDUT        uint64 = 1
	bindTH2        uint64 = 2
	bindTH3        uint64 = 3
)

// injectedMDNS is an mDNS socket that sends nowhere and receives what the
// test injects.
type injectedMDNS struct{ out chan mdns.QueryResponse }

func (f *injectedMDNS) Send([]byte) error                    { return nil }
func (f *injectedMDNS) Responses() <-chan mdns.QueryResponse { return f.out }
func (f *injectedMDNS) Close() error                         { return nil }

// announce injects the operational records of nodes, each on 127.0.0.1 at
// its port, into conn.
func announce(t *testing.T, conn *injectedMDNS, cfid [8]byte, ports map[uint64]uint16) {
	t.Helper()
	hdr := func(name string, typ uint16) dns.RR_Header {
		return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: typ, Class: dns.ClassINET | 0x8000, Ttl: 120}
	}
	msg := new(dns.Msg)
	msg.Response = true
	for id, port := range ports {
		op := discovery.OperationalQName(cfid, id)
		host := "bind-" + op[:16] + ".local"
		msg.Answer = append(msg.Answer,
			&dns.SRV{Hdr: hdr(op, dns.TypeSRV), Target: dns.Fqdn(host), Port: port},
			&dns.TXT{Hdr: hdr(op, dns.TypeTXT), Txt: []string{"SII=500", "SAI=300"}},
			&dns.A{Hdr: hdr(host, dns.TypeA), A: net.ParseIP("127.0.0.1").To4()},
		)
	}
	b, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	conn.out <- mdns.QueryResponse{Msg: b}
}

// bindNode is one in-process node of the test.
type bindNode struct {
	n    *node.Node
	cfg  node.Config
	port uint16
	ep   uint16
	plug *plug
}

func startBindNode(t *testing.T, name string, mutate func(*node.Config), add func(*node.Node) error) *bindNode {
	t.Helper()
	db := openDB(t, filepath.Join(t.TempDir(), name+".db"))
	cfg := minimalConfig(db)
	cfg.BasicInformation.NodeLabel = name
	if mutate != nil {
		mutate(&cfg)
	}
	n := newNode(t, cfg)
	if err := add(n); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	port := netip.MustParseAddrPort(n.Bridge().LocalAddr()).Port()
	return &bindNode{n: n, cfg: cfg, port: port}
}

// aclList is an AccessControl ACL value: TH1 administers, the DUT operates
// the OnOff cluster on one endpoint — the two entries of TC-BIND-2.1 step 7.
type aclList struct {
	admin, operator uint64
	endpoint        uint16
}

func (a aclList) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(1), 5) // Administer
	enc.PutUint(tlv.ContextTag(2), 2) // CASE
	enc.StartArray(tlv.ContextTag(3))
	enc.PutUint(tlv.AnonymousTag(), a.admin)
	_ = enc.EndContainer()
	enc.PutNull(tlv.ContextTag(4))
	_ = enc.EndContainer()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(1), 3) // Operate
	enc.PutUint(tlv.ContextTag(2), 2) // CASE
	enc.StartArray(tlv.ContextTag(3))
	enc.PutUint(tlv.AnonymousTag(), a.operator)
	_ = enc.EndContainer()
	enc.StartArray(tlv.ContextTag(4))
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), uint64(onoff.ClusterID))
	enc.PutUint(tlv.ContextTag(1), uint64(a.endpoint))
	enc.PutNull(tlv.ContextTag(2))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
}

// reasonLog records the reason attribute of every log record at or above
// Info — the binding manager's matter.js messages.
type reasonLog struct {
	mu  sync.Mutex
	got []string
}

func (r *reasonLog) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelInfo }

func (r *reasonLog) Handle(_ context.Context, rec slog.Record) error {
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == "reason" {
			r.mu.Lock()
			r.got = append(r.got, rec.Level.String()+" "+a.Value.String())
			r.mu.Unlock()
		}
		return true
	})
	return nil
}

func (r *reasonLog) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *reasonLog) WithGroup(string) slog.Handler      { return r }

func (r *reasonLog) has(line string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.got, line)
}

// keySetWrite is GroupKeyManagement KeySetWrite's request (Matter
// §11.2.7.1): one TrustFirst key set with epoch key 0 only.
type keySetWrite struct{ id uint16 }

func (k keySetWrite) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.StartStruct(tlv.ContextTag(0))
	enc.PutUint(tlv.ContextTag(0), uint64(k.id))
	enc.PutUint(tlv.ContextTag(1), 0) // TrustFirst
	enc.PutOctets(tlv.ContextTag(2), []byte("0123456789abcdef"))
	enc.PutUint(tlv.ContextTag(3), 1)
	for f := uint8(4); f <= 7; f++ {
		enc.PutNull(tlv.ContextTag(f))
	}
	_ = enc.EndContainer()
	_ = enc.EndContainer()
}

// groupKeyMap is a GroupKeyMap value of one mapping.
type groupKeyMap struct{ group, keySet uint16 }

func (g groupKeyMap) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(1), uint64(g.group))
	enc.PutUint(tlv.ContextTag(2), uint64(g.keySet))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
}

// bindHost is the DUT's host: what the established and removed handlers
// saw.
type bindHost struct {
	mu          sync.Mutex
	established map[uint64]binding.Resolution
	removed     []binding.Resolution
}

func (h *bindHost) onEstablished(r binding.Resolution) {
	h.mu.Lock()
	h.established[r.Entry.Node] = r
	h.mu.Unlock()
}

func (h *bindHost) onRemoved(r binding.Resolution) {
	h.mu.Lock()
	delete(h.established, r.Entry.Node)
	h.removed = append(h.removed, r)
	h.mu.Unlock()
}

func (h *bindHost) nodes() []uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]uint64, 0, len(h.established))
	for id := range h.established {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func (h *bindHost) resolutions() []binding.Resolution {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]binding.Resolution, 0, len(h.established))
	for _, r := range h.established {
		out = append(out, r)
	}
	return out
}

// switchCommand is the DUT's "switch on" / "switch off" (TC-BIND-2.1
// steps 7 and 10): the OnOff command to every established binding,
// through its resolution's interaction.
func (h *bindHost) switchCommand(ctx context.Context, t *testing.T, cmd uint32) {
	t.Helper()
	for _, r := range h.resolutions() {
		if r.Kind != binding.KindClient {
			t.Fatalf("resolution %+v", r)
		}
		res, err := r.Interaction.Invoke(ctx, im.ConcreteCommandPath{Endpoint: r.Endpoint, Cluster: onoff.ClusterID, Command: cmd}, nil, interaction.InvokeOptions{})
		if err != nil || res.Status != im.StatusSuccess {
			t.Fatalf("command 0x%02X to node %d: %+v %v", cmd, r.Entry.Node, res, err)
		}
	}
}

func attrPath(ep uint16, cluster, attribute uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func waitLive(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func onOff(srv *onoff.Server) bool {
	v, _ := srv.MatterRead(onoff.AttrOnOff)
	b, _ := v.(bool)
	return b
}

// TestLiveBinding is TC-BIND-2.1 (Test_TC_BIND_2_1.yaml at 6170af84,
// steps 2-12) in process: three node.Node commissioned into one fabric by
// the module's commissioner through client.Registry — the DUT with an
// OnOffLightSwitch (OnOff client declared) and its client stack, TH2 and
// TH3 each with an OnOff plug. TH1 writes two Binding entries on the DUT's
// switch (step 5), the DUT's host observes established (step 6), TH1
// grants the DUT Operate on the plugs (step 7, the ACL writes), the DUT
// switches on through both bindings (step 7) and both plugs are on
// (step 8); TH1 rewrites Binding with TH2's entry alone (step 9) — removed
// for TH3 — and the DUT switches off (step 10): TH2 is off (step 11), TH3
// stays on (step 12).
func TestLiveBinding(t *testing.T) {
	// Not parallel: the nodes and commissioner here starve the package's other live tests of their handshake budget.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// The DUT's DNS-SD cache, fed by the test.
	dutMDNS := &injectedMDNS{out: make(chan mdns.QueryResponse, 16)}
	dutNames := discovery.NewNames(dutMDNS, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { _ = dutNames.Close() })
	var sw *device.OnOffLightSwitch
	dutLog := &reasonLog{}
	dut := startBindNode(t, "dut", func(c *node.Config) {
		c.Client = node.Client{Enabled: true, Listen: "127.0.0.1:0", Names: dutNames}
		c.Logger = slog.New(dutLog)
	}, func(n *node.Node) error {
		var err error
		if sw, err = device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey("switch"), Name: "switch"}); err != nil {
			return err
		}
		return n.Aggregator().Add(context.Background(), sw)
	})
	targets := map[uint64]*bindNode{}
	for _, id := range []uint64{bindTH2, bindTH3} {
		p := newPlug(t, "plug")
		bn := startBindNode(t, map[uint64]string{bindTH2: "th2", bindTH3: "th3"}[id], nil, func(n *node.Node) error {
			return n.Aggregator().Add(context.Background(), p)
		})
		bn.plug = p
		var ok bool
		if bn.ep, ok = endpointOf(t, bn.n, "plug"); !ok {
			t.Fatal("the plug has no endpoint")
		}
		targets[id] = bn
	}
	switchEP, ok := endpointOf(t, dut.n, "switch")
	if !ok {
		t.Fatal("the switch has no endpoint")
	}

	// TH1: the commissioner, its fabric and the registry of client nodes.
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := exchange.NewManager(l, exchange.Config{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, stopServe := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(serveCtx, mgr.HandleDatagram)
	}()
	th1MDNS := &injectedMDNS{out: make(chan mdns.QueryResponse, 16)}
	names := discovery.NewNames(th1MDNS, slog.New(slog.DiscardHandler))
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: bindFabricID, RootNodeID: bindController, VendorID: 0xFFF1, Label: "bind"})
	if err != nil {
		t.Fatal(err)
	}
	resumption := &peer.MemoryResumptionStore{}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: resumption})
	if err != nil {
		t.Fatal(err)
	}
	subs := interaction.NewSubscriptions(interaction.SubscriptionsConfig{})
	com, err := commissioner.New(commissioner.Context{
		Exchanges: mgr, Peers: set, Attestation: attestation.TestStore(), Logger: slog.New(slog.DiscardHandler),
		Subscriptions: subs, Fabrics: []*commissioner.Fabric{fabric}, Resumption: resumption,
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := client.NewRegistry(client.RegistryConfig{
		Commissioner: com, Peers: set, Exchanges: mgr, Fabrics: []*commissioner.Fabric{fabric}, Subscriptions: subs,
		Logger: slog.New(slog.DiscardHandler), Node: client.Config{Logger: slog.New(slog.DiscardHandler)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reg.Close(context.Background())
		_ = subs.Close(context.Background())
		com.Close()
		set.Close(context.Background())
		mgr.Close()
		stopServe()
		<-served
		_ = names.Close()
	})
	cfid := fabric.Identity.CompressedFabricID
	ports := map[uint64]uint16{bindDUT: dut.port, bindTH2: targets[bindTH2].port, bindTH3: targets[bindTH3].port}
	announce(t, th1MDNS, cfid, ports)
	if err := reg.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Steps 2-4: TH1 commissions the DUT, TH2 and TH3.
	noSubscribe := false
	commissioned := map[uint64]*client.Node{}
	for _, id := range []uint64{bindDUT, bindTH2, bindTH3} {
		cn, err := reg.ForDevice(ctx, discovery.Device{
			InstanceID: map[uint64]string{bindDUT: "dut", bindTH2: "th2", bindTH3: "th3"}[id],
			Addresses:  []discovery.Address{{IP: netip.MustParseAddr("127.0.0.1"), Port: ports[id]}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.Commission(ctx, cn, client.CommissionOptions{
			Options:       commissioner.Options{Passcode: testPasscode, NodeID: id, OnAttestationFailure: attestation.Accept()},
			AutoSubscribe: &noSubscribe,
		}); err != nil {
			t.Fatalf("commissioning node %d: %v", id, err)
		}
		commissioned[id] = cn
	}
	fabrics, err := dut.n.FabricInfos(ctx)
	if err != nil || len(fabrics) != 1 || fabrics[0].NodeID != bindDUT {
		t.Fatalf("the DUT's fabrics %+v %v", fabrics, err)
	}
	dutFabric := fabrics[0].FabricIndex
	// The DUT finds TH2 and TH3 on DNS-SD.
	announce(t, dutMDNS, cfid, map[uint64]uint16{bindTH2: ports[bindTH2], bindTH3: ports[bindTH3]})

	// Step 5: TH1 writes the two Binding entries on the DUT's switch.
	b := sw.Binding().Underlying().(*mattercore.Binding)
	writeBinding := func(entries ...uint64) {
		t.Helper()
		list := make(spec.List[bindingdef.TargetStruct], 0, len(entries))
		local := make([]mattercore.TargetStruct, 0, len(entries))
		for _, id := range entries {
			node, ep, cl := id, targets[id].ep, onoff.ClusterID
			list = append(list, bindingdef.TargetStruct{Node: &node, Endpoint: &ep, Cluster: &cl})
			local = append(local, mattercore.TargetStruct{Node: id, Endpoint: ep, Cluster: cl})
		}
		st, err := commissioned[bindDUT].Write(ctx, []interaction.WriteRequest{{
			Path: attrPath(switchEP, bindingdef.ClusterID, bindingdef.AttrBinding), Value: list,
		}}, interaction.WriteOptions{DisableListChunking: true})
		if err == nil && st.Err() == nil {
			return
		}
		// The bridge decodes no Binding write today
		// (bridge/attribute_value_reader.go has no case for 0x001E, so
		// the server receives no list): until it does, the entries are
		// written on the DUT as the bridge would hand them over — on the
		// session's fabric (report AO, open question 1).
		t.Logf("Binding write over the wire: %v %v; writing it locally", st, err)
		if err := b.MatterWrite(im.WithFabricFilter(ctx, false, dutFabric), bindingdef.AttrBinding, local); err != nil {
			t.Fatal(err)
		}
	}
	writeBinding(bindTH2, bindTH3)
	if got := b.Entries(); len(got) != 2 || got[0].FabricIndex != dutFabric {
		t.Fatalf("the DUT's Binding %+v", got)
	}

	// Step 6: the DUT's host observes established — the entries woke from
	// dormancy and connect.
	host := &bindHost{established: map[uint64]binding.Resolution{}}
	dut.n.Bindings().OnRemoved(host.onRemoved)
	dut.n.Bindings().OnEstablished(host.onEstablished)
	waitLive(t, "both bindings established", func() bool { return slices.Equal(host.nodes(), []uint64{bindTH2, bindTH3}) })
	for id, r := range host.established {
		if r.SourceEndpoint != switchEP || r.Endpoint != targets[id].ep || !slices.Equal(r.Clients, []uint32{onoff.ClusterID}) || r.Peer == nil {
			t.Fatalf("resolution of node %d: %+v", id, r)
		}
	}

	// Step 7: TH1 grants the DUT Operate on each plug's OnOff, then the
	// DUT switches on through both bindings.
	for id, tn := range targets {
		st, err := commissioned[id].Write(ctx, []interaction.WriteRequest{{
			Path:  attrPath(0, 0x001F, 0x0000),
			Value: aclList{admin: bindController, operator: bindDUT, endpoint: tn.ep},
		}}, interaction.WriteOptions{})
		if err != nil || st.Err() != nil {
			t.Fatalf("ACL on node %d: %+v %v", id, st, err)
		}
	}
	host.switchCommand(ctx, t, 0x01) // On
	// Step 8 (and TH3 alike).
	if !onOff(targets[bindTH2].plug.srv) || !onOff(targets[bindTH3].plug.srv) {
		t.Fatal("a plug is not on")
	}

	// Step 9: TH1 removes TH3's entry; the DUT's host hears removed.
	writeBinding(bindTH2)
	waitLive(t, "TH3's binding removed", func() bool { return slices.Equal(host.nodes(), []uint64{bindTH2}) })
	host.mu.Lock()
	removed := slices.Clone(host.removed)
	host.mu.Unlock()
	if len(removed) != 1 || removed[0].Entry.Node != bindTH3 || removed[0].Kind != binding.KindClient {
		t.Fatalf("removed %+v", removed)
	}

	// Step 10: the DUT switches off: TH2 is off (step 11), TH3 stays on
	// (step 12).
	host.switchCommand(ctx, t, 0x00) // Off
	if onOff(targets[bindTH2].plug.srv) || !onOff(targets[bindTH3].plug.srv) {
		t.Fatalf("TH2 on %v, TH3 on %v", onOff(targets[bindTH2].plug.srv), onOff(targets[bindTH3].plug.srv))
	}

	// A group entry (TC-BIND-2.2/2.3) up to the group sender, which the
	// node does not have before brief AN: it waits for the key, and once
	// TH1 has written a key set and mapped the group, the manager asks for
	// the group's sender and, finding none, drops the entry.
	const group, keySet uint16 = 0x0101, 0x01A1
	if err := b.MatterWrite(im.WithFabricFilter(ctx, false, dutFabric), bindingdef.AttrBinding, []mattercore.TargetStruct{
		{Node: bindTH2, Endpoint: targets[bindTH2].ep, Cluster: onoff.ClusterID}, {Group: group},
	}); err != nil {
		t.Fatal(err)
	}
	waitLive(t, "the group entry waiting for its key", func() bool {
		return dutLog.has("INFO Group binding waits for the fabric to hold a key for the group")
	})
	if res, err := commissioned[bindDUT].Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003F, Command: 0x00}, keySetWrite{id: keySet}, interaction.InvokeOptions{}); err != nil || res.Status != im.StatusSuccess {
		t.Fatalf("KeySetWrite: %+v %v", res, err)
	}
	if st, err := commissioned[bindDUT].Write(ctx, []interaction.WriteRequest{{
		Path: attrPath(0, 0x003F, 0x0000), Value: groupKeyMap{group: group, keySet: keySet},
	}}, interaction.WriteOptions{DisableListChunking: true}); err != nil || st.Err() != nil {
		t.Fatalf("GroupKeyMap: %+v %v", st, err)
	}
	// Since brief AN the node has a group sender: the entry resolves to a
	// group target once the key is held.
	waitLive(t, "the group entry resolving to the group sender", func() bool {
		return slices.ContainsFunc(host.resolutions(), func(r binding.Resolution) bool { return r.Kind == binding.KindGroup })
	})

	// The node's shutdown removes the live binding.
	if err := dut.n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if got := host.nodes(); len(got) != 0 {
		t.Fatalf("still established after Stop: %v", got)
	}

	// The restart: a new node.New on the DUT's database restores the
	// switch's Binding list (nonvolatile, binding.element.ts:21-25) before
	// the binding manager registers it (BindingServer.ts:120-126), so the
	// host's handler sees both entries established again without a write.
	restarted := newNode(t, dut.cfg)
	sw2, err := device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey("switch"), Name: "switch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Aggregator().Add(ctx, sw2); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := sw2.Binding().Underlying().(*mattercore.Binding).Entries(); len(got) != 2 || got[0].Node != bindTH2 || got[1].Group != group || got[0].FabricIndex != dutFabric {
		t.Fatalf("the restored Binding list %+v", got)
	}
	again := &bindHost{established: map[uint64]binding.Resolution{}}
	restarted.Bindings().OnEstablished(again.onEstablished)
	waitLive(t, "the restored entries established", func() bool {
		return slices.Contains(again.nodes(), bindTH2) &&
			slices.ContainsFunc(again.resolutions(), func(r binding.Resolution) bool { return r.Kind == binding.KindGroup })
	})
	// The restored client entry drives the plug as before.
	host2 := &bindHost{established: map[uint64]binding.Resolution{}}
	for _, r := range again.resolutions() {
		if r.Kind == binding.KindClient {
			host2.established[r.Entry.Node] = r
		}
	}
	host2.switchCommand(ctx, t, 0x01) // On
	if !onOff(targets[bindTH2].plug.srv) {
		t.Fatal("TH2 is not on through the restored binding")
	}
}
