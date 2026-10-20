// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The proof of ADR 0020 stage 6, part b, device to device as the stage's
// others are: a controller on fabric 1 asks node A's CommissionerControl
// for approval, opens node B's window with the parameters A answers, and
// A's commissioner port — the module's own commissioner on a fabric 2 —
// commissions B. The controller plays connectedhomeip's fabric-sync admin
// (examples/fabric-sync/admin/DeviceManager.cpp:283-286,
// FabricAdmin.cpp:58-77 at the harness pin) and B the client's node.

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	admincommdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	commctrldef "github.com/SukramJ/go-fabric/cluster/spec/commissionercontrol"
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
	"github.com/SukramJ/go-fabric/transport/udp"
)

const (
	ccClientFabricID uint64 = 0x0000_0000_0000_0CC1
	ccServerFabricID uint64 = 0x0000_0000_0000_0CC2
	ccController     uint64 = 112233
	ccAdmin          uint64 = 445566
	ccNodeA          uint64 = 0x4331
	ccNodeB          uint64 = 0x4332
	ccRequestID      uint64 = 0x1234567812345678
)

// ccFabric is a controller stack — exchanges, DNS-SD cache, CA, fabric,
// peers, subscriptions, commissioner — as the other proofs' liveFabric,
// on a fabric id of its own.
type ccFabric struct {
	mgr *exchange.Manager
	com *commissioner.Commissioner
	set *peer.Set
	fab *commissioner.Fabric
	dns *groupLiveMDNS
}

func newCCFabric(t *testing.T, fabricID, rootNodeID uint64, label string) *ccFabric {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
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
	fabric, err := commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: fabricID, RootNodeID: rootNodeID, VendorID: 0xFFF1, Label: label})
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
	return &ccFabric{mgr: mgr, com: com, set: set, fab: fabric, dns: conn}
}

// commission commissions the node at addr as nodeID with passcode,
// announcing its operational record on this fabric first.
func (f *ccFabric) commission(ctx context.Context, addr *net.UDPAddr, nodeID uint64, host string, passcode uint32, timeout time.Duration) (*interaction.Interaction, error) {
	op := discovery.OperationalQName(f.fab.Identity.CompressedFabricID, nodeID)
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
		return nil, err
	}
	f.dns.out <- mdns.QueryResponse{Msg: b}
	res, err := f.com.Commission(ctx, commissioner.Options{
		Fabric: f.fab, Passcode: passcode, NodeID: nodeID, Addresses: []*net.UDPAddr{addr},
		OnAttestationFailure: attestation.Accept(), Timeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	return f.com.Interaction(f.set.Get(res.Address)), nil
}

func udpAddrOf(t *testing.T, n *node.Node) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

// ccPorts are node A's host ports: the approver approves and records; the
// commissioner commissions the client's node — found here by the test,
// where a host discovers it by the window's discriminator — onto the
// server's fabric through the module's commissioner, retrying while the
// client has not opened its window yet (fabric-sync waits
// kCommissionPrepareTimeMs, FabricAdmin.cpp:108-110).
type ccPorts struct {
	t        *testing.T
	server   *ccFabric
	clientAt *net.UDPAddr
	vid, pid uint16

	mu        sync.Mutex
	requests  []mattercore.CommissioningApprovalRequest
	done      chan error
	commissed mattercore.ReverseCommissioning
}

func (p *ccPorts) Approve(_ context.Context, r mattercore.CommissioningApprovalRequest) (bool, error) {
	p.mu.Lock()
	p.requests = append(p.requests, r)
	p.mu.Unlock()
	return true, nil
}

func (p *ccPorts) CommissionNode(ctx context.Context, rc mattercore.ReverseCommissioning) error {
	p.mu.Lock()
	p.commissed = rc
	p.mu.Unlock()
	err := p.commission(ctx, rc)
	p.done <- err
	return err
}

func (p *ccPorts) commission(ctx context.Context, rc mattercore.ReverseCommissioning) error {
	if !rc.Request.Matches(p.vid, p.pid) {
		return errors.New("the client's node is not the requested vendor and product")
	}
	ctx, cancel := context.WithTimeout(ctx, rc.ResponseTimeout)
	defer cancel()
	for {
		_, err := p.server.commission(ctx, p.clientAt, ccNodeB, "node-b-server", rc.Passcode, 3*time.Second)
		if err == nil || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// commissionerControlEndpoint is the endpoint node A serves the cluster
// on: the Aggregator's.
func commissionerControlEndpoint(t *testing.T, n *node.Node) uint16 {
	t.Helper()
	for _, ep := range n.Bridge().Topology().Endpoints {
		if ep == nil {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			if srv.MatterClusterID() == commctrldef.ClusterID {
				return ep.ID
			}
		}
	}
	t.Fatal("node A serves no CommissionerControl")
	return 0
}

// TestLiveCommissionerControl: RequestCommissioningApproval on node A over
// CASE is answered SUCCESS, the approver is asked with the controller's
// node id and fabric, and the CommissioningRequestResult reaches the
// controller over a subscription; CommissionNode answers
// ReverseOpenCommissioningWindow; the controller opens node B's window
// with those parameters (AdministratorCommissioning.
// OpenCommissioningWindow, timed — TC_CCTRL_2_2.py step 26 does the same);
// node A's commissioner port commissions B onto the server's fabric: B
// holds two fabrics, and A takes a new request.
func TestLiveCommissionerControl(t *testing.T) {
	// Not parallel: two nodes and three controllers would starve the
	// package's other live tests of their handshake budget.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	server := newCCFabric(t, ccServerFabricID, ccAdmin, "commissioner control")
	nodeB := liveNode(t, "node-b", nil)
	ports := &ccPorts{t: t, server: server, clientAt: udpAddrOf(t, nodeB), vid: 0xFFF1, pid: 0x8001, done: make(chan error, 1)}
	nodeA := liveNode(t, "node-a", func(c *node.Config) {
		c.CommissionerControl = &node.CommissionerControlConfig{
			Approver: ports, Commissioner: ports,
			SupportedDeviceCategories: commctrldef.SupportedDeviceCategoryFabricSynchronization,
		}
	})
	if nodeA.CommissionerControl() == nil || nodeB.CommissionerControl() != nil {
		t.Fatal("CommissionerControl mounted on the wrong node")
	}

	client := newCCFabric(t, ccClientFabricID, ccController, "client")
	ctlA, err := client.commission(ctx, udpAddrOf(t, nodeA), ccNodeA, "node-a", otaLivePasscode, 0)
	if err != nil {
		t.Fatalf("commission A: %v", err)
	}
	ctlB, err := client.commission(ctx, udpAddrOf(t, nodeB), ccNodeB, "node-b", otaLivePasscode, 0)
	if err != nil {
		t.Fatalf("commission B: %v", err)
	}
	epA := commissionerControlEndpoint(t, nodeA)

	// The result event, over a subscription.
	events := make(chan commctrldef.CommissioningRequestResultEvent, 4)
	sub, err := ctlA.Subscribe(ctx, interaction.SubscribeRequest{
		Events: []im.ConcreteEventPath{{
			Endpoint: epA, Cluster: commctrldef.ClusterID, Event: commctrldef.EventCommissioningRequestResult,
			HasEndpoint: true, HasCluster: true, HasEvent: true, IsUrgent: true,
		}},
		MaxIntervalCeiling: interaction.Interval(10 * time.Second),
		Updated: func(r *interaction.Report) {
			for _, ev := range r.Events {
				if !ev.HasValue {
					continue
				}
				if v, err := spec.DecodeStruct[commctrldef.CommissioningRequestResultEvent](ev.Value); err == nil {
					events <- v
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	label := "Test Ecosystem"
	res, err := ctlA.Invoke(ctx, im.ConcreteCommandPath{Endpoint: epA, Cluster: commctrldef.ClusterID, Command: commctrldef.CmdRequestCommissioningApproval},
		commctrldef.RequestCommissioningApprovalRequest{RequestId: ccRequestID, VendorId: 0xFFF1, ProductId: 0x8001, Label: &label}, interaction.InvokeOptions{})
	if err != nil || res.Status != im.StatusSuccess {
		t.Fatalf("RequestCommissioningApproval: %+v %v", res, err)
	}
	var ev commctrldef.CommissioningRequestResultEvent
	select {
	case ev = <-events:
	case <-ctx.Done():
		t.Fatal("no CommissioningRequestResult")
	}
	if ev.RequestId != ccRequestID || ev.ClientNodeId != ccController || ev.StatusCode != commctrldef.StatusSuccess || ev.FabricIndex == 0 {
		t.Fatalf("event %+v", ev)
	}
	ports.mu.Lock()
	asked := ports.requests
	ports.mu.Unlock()
	if len(asked) != 1 || asked[0].ClientNodeID != ccController || asked[0].FabricIndex != ev.FabricIndex || asked[0].Label == nil || *asked[0].Label != label {
		t.Fatalf("approver asked %+v", asked)
	}

	res, err = ctlA.Invoke(ctx, im.ConcreteCommandPath{Endpoint: epA, Cluster: commctrldef.ClusterID, Command: commctrldef.CmdCommissionNode},
		commctrldef.CommissionNodeRequest{RequestId: ccRequestID, ResponseTimeoutSeconds: 60}, interaction.InvokeOptions{})
	if err != nil || res.Command != commctrldef.CmdReverseOpenCommissioningWindow || !res.HasFields {
		t.Fatalf("CommissionNode: %+v %v", res, err)
	}
	win, err := spec.DecodeStruct[commctrldef.ReverseOpenCommissioningWindowResponse](res.Fields)
	if err != nil {
		t.Fatal(err)
	}

	// The controller opens B's window with A's parameters.
	if _, err := ctlB.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: admincommdef.ClusterID, Command: admincommdef.CmdOpenCommissioningWindow},
		admincommdef.OpenCommissioningWindowRequest{
			CommissioningTimeout: 180, PakePasscodeVerifier: win.PakePasscodeVerifier,
			Discriminator: win.Discriminator, Iterations: win.Iterations, Salt: win.Salt,
		}, interaction.InvokeOptions{Timed: true, TimedTimeout: 5 * time.Second}); err != nil {
		t.Fatalf("OpenCommissioningWindow on B: %v", err)
	}

	select {
	case err := <-ports.done:
		if err != nil {
			t.Fatalf("A's commissioner: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("A's commissioner did not finish")
	}
	ports.mu.Lock()
	rc := ports.commissed
	ports.mu.Unlock()
	if rc.Discriminator != win.Discriminator || rc.Request.RequestID != ccRequestID || rc.ResponseTimeout != time.Minute {
		t.Fatalf("commissioner asked %+v", rc)
	}
	fabrics, err := nodeB.FabricInfos(ctx)
	if err != nil || len(fabrics) != 2 {
		t.Fatalf("B's fabrics %+v (%v)", fabrics, err)
	}
	ids := map[uint64]bool{}
	for _, f := range fabrics {
		ids[f.FabricID] = true
	}
	if !ids[ccClientFabricID] || !ids[ccServerFabricID] {
		t.Fatalf("B's fabrics %+v", fabrics)
	}

	// A's request is closed: CommissionNode with it fails, a new request
	// is taken.
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err = ctlA.Invoke(ctx, im.ConcreteCommandPath{Endpoint: epA, Cluster: commctrldef.ClusterID, Command: commctrldef.CmdRequestCommissioningApproval},
			commctrldef.RequestCommissioningApprovalRequest{RequestId: ccRequestID + 1, VendorId: 0xFFF1, ProductId: 0x8001}, interaction.InvokeOptions{})
		if err == nil && res.Status == im.StatusSuccess {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("A takes no new request: %+v %v", res, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
