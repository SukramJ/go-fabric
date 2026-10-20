// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/groupcast/GroupcastServerTest.ts
// at 862ad75f: the cases that need a commissioned node, the multicast
// model of the network, a second fabric or a restart on one store. The
// cases on the server alone are in
// cluster/core/groupcast_parity_matterjs_test.go.
//
// matter.js runs them on MockServerNode with an added fabric and local
// commands (node.online with a MockExchange of the fabric); the port
// commissions the device on fabrictest and invokes the root's Groupcast
// server locally (Device.Act) with an administrator's context of the
// fabric, as the OperationalCredentials port does. MockNetwork's
// isMemberOf is Device.MulticastGroups, recordMembershipChanges is
// Network.WatchMemberships, a throwing addMembership is Network.FailJoins,
// captureLog is a slog handler on the site's logger, a reload
// (node.close and a new MockServerNode on the same environment) is
// Device.Restart, fabric.delete() is node.Node.RemoveFabric. The node's
// Groupcast server always has the Listener, Sender and PerGroup features
// (GroupcastServer.ts:81-83 as RootEndpoint installs it); the cases
// matter.js runs on its IanaOnlyRootEndpoint join with an explicit IanaAddr
// policy, so the feature set does not change what they assert.
package node_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec/groupcast"
	specgroups "github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// gcTestKey is the file's TEST_KEY, 16 zero bytes.
var gcTestKey = make([]byte, 16)

// gcNode is a commissioned device and the fabric matter.js's
// node.addFabric() gives the test.
type gcNode struct {
	site   *fabrictest.Site
	pair   *fabrictest.Pair
	dev    *fabrictest.Device
	fabric uint8
}

func newGCNode(t *testing.T, site *fabrictest.Site, topo fabrictest.Topology) *gcNode {
	t.Helper()
	p := site.CommissionedPair(fabrictest.PairOptions{Device: topo})
	return &gcNode{site: site, pair: p, dev: p.Device, fabric: fabricOf(t, p)}
}

// fabricOf is the index of the fabric pair's controller commissioned.
func fabricOf(t *testing.T, p *fabrictest.Pair) uint8 {
	t.Helper()
	fabrics, err := p.Device.Node().FabricInfos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fabrics {
		if f.FabricID == p.Controller.Fabric().FabricID {
			return f.FabricIndex
		}
	}
	t.Fatalf("no fabric of controller %d among %v", p.Controller.Index(), fabrics)
	return 0
}

// ianaJoin is the file's usual JoinGroup: one endpoint, key set 1, the
// key when it creates the set, the IanaAddr policy.
func ianaJoin(groupID uint16, endpoints []uint16, withKey bool) core.JoinGroupRequest {
	policy := groups.PolicyIanaAddr
	req := core.JoinGroupRequest{GroupID: groupID, Endpoints: endpoints, KeySetID: 1, McastAddrPolicy: &policy}
	if withKey {
		req.Key = gcTestKey
	}
	return req
}

// invoke runs a Groupcast command on the device's root as the fabric's
// administrator.
func (g *gcNode) invoke(fabric uint8, cmd uint32, fields any) (any, error) {
	var resp any
	err := g.dev.Act(func(n *node.Node) error {
		var err error
		resp, err = n.Root().Groupcast.MatterInvoke(adminContext(fabric), cmd, fields)
		return err
	})
	return resp, err
}

func (g *gcNode) join(t *testing.T, fabric uint8, req core.JoinGroupRequest) {
	t.Helper()
	if _, err := g.invoke(fabric, 0x00, req); err != nil {
		t.Fatalf("JoinGroup %d on fabric %d: %v", req.GroupID, fabric, err)
	}
}

func (g *gcNode) leave(t *testing.T, fabric uint8, groupID uint16) {
	t.Helper()
	if _, err := g.invoke(fabric, 0x01, core.LeaveGroupRequest{GroupID: groupID}); err != nil {
		t.Fatalf("LeaveGroup %d on fabric %d: %v", groupID, fabric, err)
	}
}

// membership is node.stateOf(GroupcastServer).membership.
func (g *gcNode) membership(t *testing.T) []core.GroupcastMembershipStruct {
	t.Helper()
	v, ok := g.dev.Node().Root().Groupcast.MatterRead(0x0000)
	if !ok {
		t.Fatal("Membership unreadable")
	}
	return v.([]core.GroupcastMembershipStruct)
}

func (g *gcNode) member(t *testing.T, groupID uint16) (core.GroupcastMembershipStruct, bool) {
	t.Helper()
	for _, m := range g.membership(t) {
		if m.FabricIndex == g.fabric && m.GroupID == groupID {
			return m, true
		}
	}
	return core.GroupcastMembershipStruct{}, false
}

// groupProperties is node.stateOf(GroupcastServer).groupProperties of the
// fabric, as the store holds it.
func (g *gcNode) groupProperties(t *testing.T, fabric uint8) []uint16 {
	t.Helper()
	rows, err := g.dev.Store().ListGroupcastGroups(context.Background(), fabric)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uint16, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.GroupID)
	}
	return ids
}

func (g *gcNode) address(t *testing.T, groupID uint16) net.IP {
	t.Helper()
	addr, err := g.dev.Node().Groups().MulticastAddressFor(context.Background(), g.fabric, groupID)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

// holdsGroup is network.isMemberOf(group) of the device's network.
func holdsGroup(d *fabrictest.Device, group net.IP) bool {
	return slices.ContainsFunc(d.MulticastGroups(), group.Equal)
}

// watchMemberships is recordMembershipChanges (multicast-helpers.ts): the
// joins and leaves from now on, as "join <ip>" / "leave <ip>".
func watchMemberships(t *testing.T, site *fabrictest.Site) func() []string {
	t.Helper()
	var (
		mu      sync.Mutex
		changes []string
	)
	stop := site.Net().WatchMemberships(func(c fabrictest.MembershipChange) {
		mu.Lock()
		defer mu.Unlock()
		changes = append(changes, c.String())
	})
	t.Cleanup(stop)
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(changes)
	}
}

// logCapture records every log record of the site's hosts: matter.js's
// captureLog and Logger.destinations.capture.
type logCapture struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (*logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, r.Clone())
	return nil
}
func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *logCapture) WithGroup(string) slog.Handler      { return c }

// count is the number of records at level or above whose message is msg
// ("" for any).
func (c *logCapture) count(level slog.Level, msg string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for i := range c.recs {
		r := &c.recs[i]
		if r.Level >= level && (msg == "" || r.Message == msg) {
			n++
		}
	}
	return n
}

func (c *logCapture) messages(level slog.Level) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for i := range c.recs {
		r := &c.recs[i]
		if r.Level >= level {
			out = append(out, r.Message)
		}
	}
	return out
}

// capturedSite is a site whose hosts log into the returned capture.
func capturedSite(t *testing.T) (*fabrictest.Site, *logCapture) {
	t.Helper()
	holdSite(t)
	c := &logCapture{}
	return fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(c)}), c
}

// TestGroupcastMulticastPolicyOrderingParityMatterJS ports the describe
// "multicast policy ordering" (GroupcastServerTest.ts:1366-1421).
func TestGroupcastMulticastPolicyOrderingParityMatterJS(t *testing.T) {
	holdSite(t)

	// :1367-1389. matter.js reads the group's address when its key map
	// entry appears (GroupcastServerTest.ts:1374-1376); what that address
	// decides is the multicast join, so the port asserts the joins the
	// network sees: the group's own address from the first one on, no
	// join of the default per-group address and rebind. The policy is set
	// before the endpoints (GroupcastServer.ts:253-258).
	t.Run("sets policy before groupKeyIdMap.added fires (listeners see correct address immediately)", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		changes := watchMemberships(t, g.site)
		g.join(t, g.fabric, ianaJoin(0x0001, []uint16{1}, true))
		if got := changes(); !slices.Equal(got, []string{"join ff05::fa"}) {
			t.Errorf("membership changes %v, want [join ff05::fa]", got)
		}
		if got := g.address(t, 0x0001); !got.Equal(groups.IANAGroupcastAddress) {
			t.Errorf("address %v, want the IANA address", got)
		}
	})

	// :1391-1420. The fabric is replaced by UpdateNOC with the same
	// fabric ID — matter.js FabricManager.replaceFabric.
	t.Run("keeps multicast policy and membership when a fabric is replaced", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		g.join(t, g.fabric, ianaJoin(0x0001, []uint16{1}, true))
		changes := watchMemberships(t, g.site)
		if !holdsGroup(g.dev, groups.IANAGroupcastAddress) {
			t.Fatalf("the device holds %v, want the IANA address", g.dev.MulticastGroups())
		}
		fabrics, err := g.dev.Node().FabricInfos(parityCtx(t))
		if err != nil {
			t.Fatal(err)
		}
		if got := updateNOC(t, g.pair, g.pair.Controller.Fabric().FabricID, fabrics[0].NodeID); got != uint64(core.NOCStatusOK) {
			t.Fatalf("UpdateNOC StatusCode %d, want Ok", got)
		}
		if got := g.address(t, 0x0001); !got.Equal(groups.IANAGroupcastAddress) {
			t.Errorf("address after the replacement %v, want the IANA address", got)
		}
		if m, ok := g.member(t, 0x0001); !ok || m.KeySetID == nil || *m.KeySetID != 1 {
			t.Errorf("membership after the replacement %+v (%v), want key set 1", m, ok)
		}
		if err := g.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if got := changes(); len(got) != 0 {
			t.Errorf("membership changes %v, want none", got)
		}
		if !holdsGroup(g.dev, groups.IANAGroupcastAddress) {
			t.Errorf("after the replacement the device holds %v", g.dev.MulticastGroups())
		}
	})
}

// TestGroupcastMulticastRebindParityMatterJS ports the describe
// "order-independent multicast rebind" (GroupcastServerTest.ts:1423-1469).
func TestGroupcastMulticastRebindParityMatterJS(t *testing.T) {
	holdSite(t)
	t.Run("rebinds a listener group's actual multicast membership to the applied policy after a full reload", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		defaultAddress := g.address(t, 0x0001)
		if defaultAddress.Equal(groups.IANAGroupcastAddress) {
			t.Fatalf("the default address of a group without policy is %v", defaultAddress)
		}
		g.join(t, g.fabric, ianaJoin(0x0001, []uint16{1}, true))
		if !holdsGroup(g.dev, groups.IANAGroupcastAddress) || holdsGroup(g.dev, defaultAddress) {
			t.Fatalf("before the reload the device holds %v", g.dev.MulticastGroups())
		}
		if err := g.dev.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if !holdsGroup(g.dev, groups.IANAGroupcastAddress) || holdsGroup(g.dev, defaultAddress) {
			t.Errorf("after the reload the device holds %v, want the IANA address and not %v", g.dev.MulticastGroups(), defaultAddress)
		}
	})
}

// TestGroupcastShutdownCleanupParityMatterJS ports the describe "shutdown
// cleanup" (GroupcastServerTest.ts:1471-1499).
func TestGroupcastShutdownCleanupParityMatterJS(t *testing.T) {
	holdSite(t)
	t.Run("drops joined multicast memberships on shutdown so the socket can close", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		g.join(t, g.fabric, ianaJoin(0x0001, []uint16{1}, true))
		if !holdsGroup(g.dev, groups.IANAGroupcastAddress) {
			t.Fatalf("the device holds %v, want the IANA address", g.dev.MulticastGroups())
		}
		if err := g.dev.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if holdsGroup(g.dev, groups.IANAGroupcastAddress) {
			t.Errorf("after shutdown the device still holds %v", g.dev.MulticastGroups())
		}
	})
}

// TestGroupcastMulticastAcrossFabricsParityMatterJS ports the describe
// "multicast membership across fabrics" (GroupcastServerTest.ts:1501-1540):
// the second fabric is a second controller's.
func TestGroupcastMulticastAcrossFabricsParityMatterJS(t *testing.T) {
	holdSite(t)
	t.Run("keeps a shared IANA address joined while another fabric still uses it", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		fabric2 := fabricOf(t, secondFabric(t, g.site, g.pair))
		g.join(t, g.fabric, ianaJoin(0x0001, []uint16{1}, true))
		g.join(t, fabric2, ianaJoin(0x0001, []uint16{1}, true))
		if !holdsGroup(g.dev, groups.IANAGroupcastAddress) {
			t.Fatalf("the device holds %v", g.dev.MulticastGroups())
		}
		g.leave(t, fabric2, 0x0001)
		if !holdsGroup(g.dev, groups.IANAGroupcastAddress) {
			t.Errorf("fabric 2 leaving dropped the address fabric 1 still uses: %v", g.dev.MulticastGroups())
		}
		g.leave(t, g.fabric, 0x0001)
		if holdsGroup(g.dev, groups.IANAGroupcastAddress) {
			t.Errorf("the address stayed joined after the last fabric left: %v", g.dev.MulticastGroups())
		}
	})
}

// joinFailureMessage is the bridge's log message of a failed multicast
// join — matter.js's "Failed to join multicast address …".
const joinFailureMessage = "matter.groups.join_failed"

// TestGroupcastMulticastJoinFailuresParityMatterJS ports the describe
// "multicast join failures" (GroupcastServerTest.ts:1591-1710). The join
// fails on the network (Network.FailJoins), the retry is the bridge's
// 30-second timer on the site's fake clock (MockTime.advance(30_000)), and
// joinFailures counts the join-failure records at info or above.
func TestGroupcastMulticastJoinFailuresParityMatterJS(t *testing.T) {
	holdSite(t)
	simulated := errors.New("simulated multicast join failure")

	type setup struct {
		*gcNode
		failing *atomic.Bool
		log     *logCapture
	}
	nodeWithFailingJoins := func(t *testing.T) setup {
		t.Helper()
		site, log := capturedSite(t)
		failing := &atomic.Bool{}
		failing.Store(true)
		site.Net().FailJoins(func(net.IP) error {
			if failing.Load() {
				return simulated
			}
			return nil
		})
		return setup{gcNode: newGCNode(t, site, fabrictest.Topology{}), failing: failing, log: log}
	}
	retry := func(t *testing.T, s setup) {
		t.Helper()
		s.site.Fake().Advance(30 * time.Second)
		if err := s.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
	}
	joinFailures := func(s setup) int { return s.log.count(slog.LevelInfo, joinFailureMessage) }

	t.Run("retries a failed multicast join without another group change", func(t *testing.T) {
		s := nodeWithFailingJoins(t)
		s.join(t, s.fabric, ianaJoin(0x0001, []uint16{1}, true))
		if holdsGroup(s.dev, groups.IANAGroupcastAddress) {
			t.Fatal("a failing join joined")
		}
		s.failing.Store(false)
		retry(t, s)
		if !holdsGroup(s.dev, groups.IANAGroupcastAddress) {
			t.Errorf("the retry did not join: %v", s.dev.MulticastGroups())
		}
	})

	t.Run("logs a failing multicast join at info once per failure streak", func(t *testing.T) {
		s := nodeWithFailingJoins(t)
		s.join(t, s.fabric, ianaJoin(0x0001, []uint16{1}, true))
		retry(t, s)
		retry(t, s)
		if got := joinFailures(s); got != 1 {
			t.Errorf("join failures at info after two retries: %d, want 1", got)
		}
		s.failing.Store(false)
		retry(t, s)
		if !holdsGroup(s.dev, groups.IANAGroupcastAddress) {
			t.Fatalf("the retry did not join: %v", s.dev.MulticastGroups())
		}
		s.leave(t, s.fabric, 0x0001)
		s.failing.Store(true)
		s.join(t, s.fabric, ianaJoin(0x0001, []uint16{1}, false))
		retry(t, s)
		if got := joinFailures(s); got != 2 {
			t.Errorf("join failures at info after the second streak: %d, want 2", got)
		}
	})

	t.Run("starts a new failure streak for an address that was not wanted in between", func(t *testing.T) {
		s := nodeWithFailingJoins(t)
		s.join(t, s.fabric, ianaJoin(0x0001, []uint16{1}, true))
		if got := joinFailures(s); got != 1 {
			t.Errorf("join failures at info: %d, want 1", got)
		}
		s.leave(t, s.fabric, 0x0001)
		s.join(t, s.fabric, ianaJoin(0x0001, []uint16{1}, false))
		if got := joinFailures(s); got != 2 {
			t.Errorf("join failures at info after the address was unwanted: %d, want 2", got)
		}
	})
}

// TestGroupcastFabricRemovalParityMatterJS ports the two cases of the
// describe "legacy removal prune" that remove a fabric
// (GroupcastServerTest.ts:1124-1194); fabric.delete() is
// node.Node.RemoveFabric.
func TestGroupcastFabricRemovalParityMatterJS(t *testing.T) {
	holdSite(t)

	t.Run("removes groupProperties when the owning fabric departs", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		g.join(t, g.fabric, ianaJoin(0x0105, []uint16{1}, true))
		if len(g.groupProperties(t, g.fabric)) == 0 {
			t.Fatal("JoinGroup recorded no groupProperties")
		}
		if err := g.dev.Act(func(n *node.Node) error { return n.RemoveFabric(context.Background(), g.fabric) }); err != nil {
			t.Fatal(err)
		}
		if got := g.groupProperties(t, g.fabric); len(got) != 0 {
			t.Errorf("groupProperties of the departed fabric: %v", got)
		}
	})

	// :1147-1194. A second fabric survives, as matter.js needs for its
	// fabric-scoped sanitizer to run. The transaction conflict matter.js
	// captures from its log is its reactor runtime's; the port asserts the
	// node logs no error while the fabric goes and the observable result.
	t.Run("removes a fabric with joined groups without a transaction conflict", func(t *testing.T) {
		site, log := capturedSite(t)
		g := newGCNode(t, site, fabrictest.Topology{})
		secondFabric(t, g.site, g.pair)
		req := ianaJoin(0x0108, []uint16{1}, true)
		req.UseAuxiliaryACL = &[]bool{true}[0]
		g.join(t, g.fabric, req)
		if _, ok := g.member(t, 0x0108); !ok {
			t.Fatal("the group is no member")
		}
		before := len(log.messages(slog.LevelError))
		if err := g.dev.Act(func(n *node.Node) error { return n.RemoveFabric(context.Background(), g.fabric) }); err != nil {
			t.Fatal(err)
		}
		if err := g.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if errs := log.messages(slog.LevelError)[before:]; len(errs) != 0 {
			t.Errorf("errors logged while the fabric was removed: %v", errs)
		}
		for _, m := range g.membership(t) {
			if m.FabricIndex == g.fabric {
				t.Errorf("membership of the removed fabric: %+v", m)
			}
		}
		if got := g.groupProperties(t, g.fabric); len(got) != 0 {
			t.Errorf("groupProperties of the removed fabric: %v", got)
		}
	})
}

// TestGroupcastDerivedMembershipReloadParityMatterJS ports the reload
// cases of the describe "derived membership" (GroupcastServerTest.ts:
// 1943-1974, 2002-2041, 2043-2143).
func TestGroupcastDerivedMembershipReloadParityMatterJS(t *testing.T) {
	holdSite(t)

	t.Run("Membership survives reload via derived sources", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		req := ianaJoin(0x0102, []uint16{1}, true)
		req.KeySetID, req.UseAuxiliaryACL = 3, &[]bool{true}[0]
		g.join(t, g.fabric, req)
		before := g.membership(t)
		if err := g.dev.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if after := g.membership(t); !reflect.DeepEqual(after, before) {
			t.Errorf("Membership after the reload %+v, want %+v", after, before)
		}
	})

	t.Run("groupcast group with unmapped key persists across reload", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		g.join(t, g.fabric, ianaJoin(0x0202, []uint16{1}, true))
		if err := g.dev.Act(func(n *node.Node) error {
			return n.Root().GroupKeyManagement.MatterWrite(adminContext(g.fabric), 0x0000, []core.GroupKeyMapStruct{})
		}); err != nil {
			t.Fatalf("clearing GroupKeyMap: %v", err)
		}
		if m, ok := g.member(t, 0x0202); !ok || m.KeySetID == nil || *m.KeySetID != groups.UnmappedKeySetID {
			t.Fatalf("before the reload: %+v (%v), want KeySetId 0xFFFF", m, ok)
		}
		if err := g.dev.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if m, ok := g.member(t, 0x0202); !ok || m.KeySetID == nil || *m.KeySetID != groups.UnmappedKeySetID {
			t.Errorf("after the reload: %+v (%v), want the member with KeySetId 0xFFFF", m, ok)
		}
	})

	t.Run("networking and auxiliary ACLs reconstruct identically after reload", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		perGroup := groups.PolicyPerGroup
		g.join(t, g.fabric, core.JoinGroupRequest{
			GroupID: 0x0203, Endpoints: []uint16{1, 2}, KeySetID: 1, Key: gcTestKey,
			UseAuxiliaryACL: &[]bool{true}[0], McastAddrPolicy: &perGroup,
		})
		g.join(t, g.fabric, ianaJoin(0x0205, []uint16{1}, false))
		g.join(t, g.fabric, ianaJoin(0x0204, []uint16{1}, false))
		// GroupKeyManagementServer.removeEndpoint(fabric, 1, 0x0204), the
		// path Groups.RemoveGroup takes.
		if err := g.dev.Act(func(n *node.Node) error {
			_, err := n.Groups().RemoveEndpoint(context.Background(), g.fabric, 1, 0x0204, false)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, ok := g.member(t, 0x0204); ok {
			t.Fatal("the removed group is still a member")
		}
		snapshot := func() (map[uint16][]uint16, net.IP, net.IP, []core.AccessControlAuxiliaryEntryStruct) {
			t.Helper()
			table, err := g.dev.Node().Groups().GroupTable(context.Background(), g.fabric)
			if err != nil {
				t.Fatal(err)
			}
			eps := map[uint16][]uint16{}
			for _, e := range table {
				eps[e.GroupID] = slices.Sorted(slices.Values(e.Endpoints))
			}
			v, ok := g.dev.Node().Root().AccessControl.MatterRead(0x0007)
			if !ok {
				t.Fatal("AuxiliaryAcl unreadable")
			}
			var aux []core.AccessControlAuxiliaryEntryStruct
			for _, e := range v.([]core.AccessControlAuxiliaryEntryStruct) {
				if e.Entry.FabricIndex == g.fabric {
					aux = append(aux, e)
				}
			}
			return eps, g.address(t, 0x0203), g.address(t, 0x0205), aux
		}
		epsBefore, perGroupBefore, ianaBefore, auxBefore := snapshot()
		if want := map[uint16][]uint16{0x0203: {1, 2}, 0x0205: {1}}; !reflect.DeepEqual(epsBefore, want) {
			t.Fatalf("endpoints before the reload %v, want %v", epsBefore, want)
		}
		if !strings.HasPrefix(perGroupBefore.String(), "ff35:") || !ianaBefore.Equal(groups.IANAGroupcastAddress) || len(auxBefore) != 1 {
			t.Fatalf("before the reload: per-group %v, IANA %v, auxiliary ACL %+v", perGroupBefore, ianaBefore, auxBefore)
		}
		if err := g.dev.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		epsAfter, perGroupAfter, ianaAfter, auxAfter := snapshot()
		if !reflect.DeepEqual(epsAfter, epsBefore) || !perGroupAfter.Equal(perGroupBefore) || !ianaAfter.Equal(ianaBefore) {
			t.Errorf("after the reload: endpoints %v, per-group %v, IANA %v", epsAfter, perGroupAfter, ianaAfter)
		}
		if !reflect.DeepEqual(auxAfter, auxBefore) {
			t.Errorf("auxiliary ACL after the reload %+v, want %+v", auxAfter, auxBefore)
		}
		if _, ok := g.member(t, 0x0204); ok {
			t.Error("the removed group reappeared after the reload")
		}
	})
}

// TestGroupcastPersistenceParityMatterJS ports the describe "persistence"
// (GroupcastServerTest.ts:2170-2202): a groupProperties entry written
// locally — groups.Manager.SetGroupProperties, the writer of the state
// matter.js sets — is there after a reload.
func TestGroupcastPersistenceParityMatterJS(t *testing.T) {
	holdSite(t)
	t.Run("persists groupProperties across reload", func(t *testing.T) {
		g := newGCNode(t, quietSite(t), fabrictest.Topology{})
		iana, noAux := groups.PolicyIanaAddr, false
		if err := g.dev.Act(func(n *node.Node) error {
			return n.Groups().SetGroupProperties(context.Background(), g.fabric, 0x0101, &iana, &noAux)
		}); err != nil {
			t.Fatal(err)
		}
		if err := g.dev.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if got := g.groupProperties(t, g.fabric); !slices.Equal(got, []uint16{0x0101}) {
			t.Errorf("groupProperties after the reload %v, want [0x0101]", got)
		}
		if m, ok := g.member(t, 0x0101); !ok || m.McastAddrPolicy != groups.PolicyIanaAddr {
			t.Errorf("Membership derived after the reload %+v (%v)", m, ok)
		}
	})
}

// TestGroupcastAuxiliaryAccessUpdatedAdminParityMatterJS ports the describe
// "AuxiliaryAccessUpdated admin (CASE session)"
// (GroupcastServerTest.ts:2204-2242): JoinGroup and the legacy
// RemoveAllGroups go over CASE from the commissioning controller, and the
// AuxiliaryAccessUpdated events the removal raises name the controller's
// node id (the fabric's root node id).
func TestGroupcastAuxiliaryAccessUpdatedAdminParityMatterJS(t *testing.T) {
	holdSite(t)
	t.Run("names the administering node when legacy RemoveAllGroups empties a listener group", func(t *testing.T) {
		site := quietSite(t)
		light := newLight(t, site, "light:1")
		g := newGCNode(t, site, fabrictest.Topology{Bridged: []node.Endpoint{light}})
		ep, ok := g.dev.EndpointOf("light:1")
		if !ok {
			t.Fatal("the light has no endpoint")
		}
		ctx := parityCtx(t)
		key, aux, policy := gcTestKey, true, groupcast.MulticastAddrPolicyEnum(groups.PolicyIanaAddr)
		if _, err := fabrictest.Invoke(ctx, g.pair, im.ConcreteCommandPath{Endpoint: 0, Cluster: groupcast.ClusterID, Command: groupcast.CmdJoinGroup},
			groupcast.JoinGroupRequest{GroupId: 0x0101, Endpoints: []uint16{ep}, KeySetId: 1, Key: &key, UseAuxiliaryAcl: &aux, McastAddrPolicy: &policy}); err != nil {
			t.Fatalf("JoinGroup over CASE: %v", err)
		}
		seen := len(auxiliaryAccessUpdated(g.dev.Node()))
		if _, err := fabrictest.Invoke(ctx, g.pair, im.ConcreteCommandPath{Endpoint: ep, Cluster: specgroups.ClusterID, Command: specgroups.CmdRemoveAllGroups},
			specgroups.RemoveAllGroupsRequest{}); err != nil {
			t.Fatalf("RemoveAllGroups over CASE: %v", err)
		}
		admins := auxiliaryAccessUpdated(g.dev.Node())[seen:]
		if len(admins) == 0 {
			t.Fatal("RemoveAllGroups raised no AuxiliaryAccessUpdated")
		}
		root := g.pair.Controller.Fabric().RootNodeID
		for _, a := range admins {
			if a.AdminNodeID == nil || *a.AdminNodeID != root {
				t.Errorf("AuxiliaryAccessUpdated %+v, want AdminNodeID %d", a, root)
			}
		}
	})
}

// auxiliaryAccessUpdated is every AuxiliaryAccessUpdated event in the
// node's event log, oldest first.
func auxiliaryAccessUpdated(n *node.Node) []core.AuxiliaryAccessUpdatedEvent {
	var out []core.AuxiliaryAccessUpdatedEvent
	for _, rec := range n.Bridge().EventLog().Query(0, 0x001F, 0x0003, 0) {
		if ev, ok := rec.Payload.(core.AuxiliaryAccessUpdatedEvent); ok {
			out = append(out, ev)
		}
	}
	return out
}
