// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"net"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestDeviceMulticastGroups: a device's bridge joins the multicast address
// of a group with a member endpoint on the in-memory network, which
// Device.MulticastGroups and Network.Members report, and leaves it when
// the node stops — the surface the ported GroupcastServerTest.ts cases
// assert on, as matter.js's assert on MockNetwork.isMemberOf.
func TestDeviceMulticastGroups(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	p := site.CommissionedPair(fabrictest.PairOptions{})
	var (
		mu      sync.Mutex
		changes []string
	)
	stop := site.Net().WatchMemberships(func(c fabrictest.MembershipChange) {
		mu.Lock()
		defer mu.Unlock()
		changes = append(changes, c.String())
	})
	defer stop()
	fabrics, err := p.Device.Node().Fabrics(ctx)
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %v, %v", fabrics, err)
	}
	fabric := fabrics[0].FabricIndex
	policy := groups.PolicyIanaAddr
	if err := p.Device.Act(func(n *node.Node) error {
		admin := im.WithAuthority(im.WithSubject(im.WithFabricFilter(context.Background(), true, fabric), 112233, nil),
			func(uint16, uint32, uint8) im.StatusCode { return im.StatusSuccess })
		_, err := n.Root().Groupcast.MatterInvoke(admin, 0x00, core.JoinGroupRequest{
			GroupID: 1, Endpoints: []uint16{1}, KeySetID: 1, Key: make([]byte, 16), McastAddrPolicy: &policy,
		})
		return err
	}); err != nil {
		t.Fatalf("JoinGroup: %v", err)
	}
	iana := groups.IANAGroupcastAddress
	if err := site.Eventually(ctx, "the IANA address joined", func() bool { return site.Net().Members(iana) == 1 }); err != nil {
		t.Fatal(err)
	}
	if got := p.Device.MulticastGroups(); len(got) != 1 || !got[0].Equal(iana) {
		t.Fatalf("MulticastGroups = %v, want [%v]", got, iana)
	}
	if err := p.Device.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if got := p.Device.MulticastGroups(); len(got) != 0 {
		t.Errorf("after Stop the device holds %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(changes, "join ff05::fa") || changes[len(changes)-1] != "leave ff05::fa" {
		t.Errorf("membership changes %v, want the join of ff05::fa and its leave last", changes)
	}
	if got := site.Net().Members(net.ParseIP("ff05::fa")); got != 0 {
		t.Errorf("Members after Stop = %d", got)
	}
}

// TestDeviceGroupSend: a device's group messages go onto the in-memory
// network from its address, one datagram to the group's multicast address
// on port 5540 that Network.Filter sees — the transport the ported
// BindingIntegrationTest.ts "group sends" cases run on, as matter.js's run
// on MockUdpSocket.send.
func TestDeviceGroupSend(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	p := site.CommissionedPair(fabrictest.PairOptions{})
	fabrics, err := p.Device.Node().Fabrics(ctx)
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %v, %v", fabrics, err)
	}
	fabric := fabrics[0].FabricIndex
	policy := groups.PolicyIanaAddr
	if err := p.Device.Act(func(n *node.Node) error {
		admin := im.WithAuthority(im.WithSubject(im.WithFabricFilter(context.Background(), true, fabric), 112233, nil),
			func(uint16, uint32, uint8) im.StatusCode { return im.StatusSuccess })
		_, err := n.Root().Groupcast.MatterInvoke(admin, 0x00, core.JoinGroupRequest{
			GroupID: 1, Endpoints: []uint16{1}, KeySetID: 1, Key: make([]byte, 16), McastAddrPolicy: &policy,
		})
		return err
	}); err != nil {
		t.Fatalf("JoinGroup: %v", err)
	}
	var (
		mu   sync.Mutex
		sent []fabrictest.Datagram
	)
	site.Net().Filter(func(d fabrictest.Datagram) bool {
		if d.To.IP.IsMulticast() {
			mu.Lock()
			sent = append(sent, d)
			mu.Unlock()
		}
		return true
	})
	t.Cleanup(func() { site.Net().Filter(nil) })
	s, err := p.Device.Node().Groups().Sender(ctx, fabric, 1)
	if err != nil {
		t.Fatalf("Sender: %v", err)
	}
	if err := s.Invoke(ctx, im.ConcreteCommandPath{Cluster: 0x0006, Command: 0x01}, nil, groups.RequestOptions{}); err != nil {
		t.Fatalf("group Invoke: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("%d group datagrams on the network, want 1", len(sent))
	}
	if d := sent[0]; !d.From.IP.Equal(p.Device.Addr().IP) || !d.To.IP.Equal(groups.IANAGroupcastAddress) || d.To.Port != 5540 {
		t.Errorf("group datagram from %v to %v, want from %v to [%v]:5540", d.From, d.To, p.Device.Addr(), groups.IANAGroupcastAddress)
	}
}
