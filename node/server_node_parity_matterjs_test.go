// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ServerNodeTest.ts at 862ad75f:
// the server node end to end — commissioning, its timeout, decommissioning
// and recommissioning, a second fabric and the removal of the first with its
// fabric-scoped data, persisted values across a restart, and the Aggregator's
// composition.
//
// matter.js drives a MockServerNode through CommissioningHelper
// (packages/node/src/testing/node-helpers.ts:181) on mocked exchanges; the
// port commissions over the wire on fabrictest, a second fabric by a second
// controller after the device opened a window (Device.OpenWindow), and reads
// the device's fabric-scoped state locally — the node's fabrics, its ACL
// store, its event log — as matter.js reads node.state. Most of the file's
// other cases test the matter.js node runtime (lifecycle events, storage
// locks, behavior crashes, endpoint-number presets, the controller's factory
// reset of peers and CA material) and are listed missing in the register
// with their reason.
package node_test

import (
	"context"
	"net"
	"reflect"
	"slices"
	"testing"
	"time"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/tlv"
)

// removeFabricFields is OperationalCredentials.RemoveFabric's request.
type removeFabricFields struct{ fabricIndex uint8 }

func (r removeFabricFields) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(r.fabricIndex))
	_ = enc.EndContainer()
}

// armFailSafeFields is GeneralCommissioning.ArmFailSafe's request.
type armFailSafeFields struct {
	expiry     uint16
	breadcrumb uint64
}

func (a armFailSafeFields) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(a.expiry))
	enc.PutUint(tlv.ContextTag(1), a.breadcrumb)
	_ = enc.EndContainer()
}

const (
	gencommClusterID       uint32 = 0x0030
	opCredsClusterID       uint32 = 0x003E
	opCredsCmdRemoveFabric uint32 = 0x0A
)

// fabricIndexOf is the device's index of the pair's fabric.
func fabricIndexOf(t *testing.T, p *fabrictest.Pair) uint8 {
	t.Helper()
	fabrics, err := p.Device.Node().Fabrics(parityCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fabrics {
		if f.NodeID == p.Address.NodeID && f.FabricID == p.Controller.Fabric().FabricID {
			return f.FabricIndex
		}
	}
	t.Fatalf("the device holds no fabric of controller %d", p.Controller.Index())
	return 0
}

// secondFabric commissions p's device onto a second controller's fabric.
func secondFabric(t *testing.T, site *fabrictest.Site, p *fabrictest.Pair) *fabrictest.Pair {
	t.Helper()
	ctx := parityCtx(t)
	if err := p.Device.OpenWindow(ctx, 0); err != nil {
		t.Fatal(err)
	}
	p2, err := site.AddController(fabrictest.ControllerOptions{FabricID: 2, NodeID: 0x2222}).Commission(ctx, p.Device, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return p2
}

// eventsOfFabric counts the device's events whose payload carries the
// fabric index, matter.js's occurrencesPerFabric.
func eventsOfFabric(n *node.Node, fabricIndex uint8, skip func(cluster, event uint32) bool) int {
	count := 0
	for _, rec := range n.Bridge().EventLog().Query(0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0) {
		if skip != nil && skip(rec.Cluster, rec.EventID) {
			continue
		}
		v := reflect.ValueOf(rec.Payload)
		for v.Kind() == reflect.Pointer && !v.IsNil() {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			continue
		}
		if f := v.FieldByName("FabricIndex"); f.IsValid() && f.CanUint() && f.Uint() == uint64(fabricIndex) {
			count++
		}
	}
	return count
}

func TestServerNodeParity(t *testing.T) {
	t.Parallel()

	t.Run("commissions", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		if err := p.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("times out commissioning", func(t *testing.T) {
		t.Parallel()
		// CommissioningHelper.almostCommission: AddNOC, no
		// CommissioningComplete — the flow stopped at its transition.
		site := quietSite(t)
		u := site.UncommissionedPair(fabrictest.PairOptions{})
		ctx := parityCtx(t)
		if err := site.Run(ctx, func(ctx context.Context) error {
			_, err := u.Controller.Commissioner().Commission(ctx, commissioner.Options{
				Fabric: u.Controller.Fabric(), Passcode: u.Device.Passcode(), Addresses: []*net.UDPAddr{u.Device.Addr()},
				FinalizeCommissioning: func(context.Context, handshake.PeerAddress, *discovery.Device, *sessionparams.Parameters) error {
					return nil
				},
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		fabrics := func() int {
			f, err := u.Device.Node().Fabrics(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return len(f)
		}
		if n := fabrics(); n != 1 {
			t.Fatalf("%d commissioned fabrics after AddNOC, want 1", n)
		}
		// The fail-safe the commissioner armed: the device's own
		// FailSafeExpiryLengthSeconds (900 s), where matter.js's helper arms
		// FAILSAFE_LENGTH_S (60 s).
		site.Fake().Advance(16 * time.Minute)
		if err := site.Eventually(ctx, "the fabric rolled back", func() bool { return fabrics() == 0 }); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("decommissions and recommissions", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		ctx := parityCtx(t)
		idx := fabricIndexOf(t, p)
		if _, err := fabrictest.Invoke(ctx, p, cmdPath(0, opCredsClusterID, opCredsCmdRemoveFabric), removeFabricFields{idx}); err != nil {
			t.Logf("RemoveFabric answered %v (the session goes with the fabric)", err)
		}
		if err := site.Eventually(ctx, "the node decommissioned", func() bool {
			f, err := p.Device.Node().Fabrics(ctx)
			return err == nil && len(f) == 0
		}); err != nil {
			t.Fatal(err)
		}
		// The node opens its own window again once the last fabric left
		// (node.reopenIfDecommissioned), so a controller commissions it
		// anew.
		var again *fabrictest.Pair
		if err := site.Eventually(ctx, "a recommissioning", func() bool {
			c, err := p.Controller.Commission(ctx, p.Device, fabrictest.CommissionOptions{})
			again = c
			return err == nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := fabrictest.Read(ctx, again, attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("commissions twice", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		p2 := secondFabric(t, site, p)
		ctx := parityCtx(t)
		f, err := p.Device.Node().Fabrics(ctx)
		if err != nil || len(f) != 2 {
			t.Fatalf("fabrics %+v, %v, want 2", f, err)
		}
		if fabricIndexOf(t, p2) != 2 {
			t.Fatalf("the second fabric has index %d, want 2", fabricIndexOf(t, p2))
		}
		// CommissionedFabrics (0x0003) and Fabrics (0x0001), read
		// unfiltered.
		r, err := fabrictest.ReadWith(ctx, p2, interactionRead(true, attrPath(0, opCredsClusterID, 0x0003), attrPath(0, opCredsClusterID, 0x0001), attrPath(0, opCredsClusterID, 0x0000)))
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := r.Value(0, opCredsClusterID, 0x0003); v.El.Uint != 2 {
			t.Fatalf("CommissionedFabrics %v, want 2", v.El.Uint)
		}
		if n, _ := listLen(r, 0, opCredsClusterID, 0x0001); n != 2 {
			t.Fatalf("%d Fabrics, want 2", n)
		}
		if n, _ := listLen(r, 0, opCredsClusterID, 0x0000); n != 2 {
			t.Fatalf("%d NOCs, want 2", n)
		}
	})

	t.Run("commissions twice and removes first including fabric scoped data", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		p2 := secondFabric(t, site, p)
		ctx := parityCtx(t)
		n := p.Device.Node()
		aclOf := func(idx uint8) int {
			acl, err := n.Store().ListACL(ctx, idx)
			if err != nil {
				t.Fatal(err)
			}
			return len(acl)
		}
		first, second := fabricIndexOf(t, p), fabricIndexOf(t, p2)
		if aclOf(first) != 1 || aclOf(second) != 1 {
			t.Fatalf("ACL entries %d, %d, want 1 each", aclOf(first), aclOf(second))
		}
		leave := func(cluster, event uint32) bool {
			return cluster == basicinfo.ClusterID && event == basicinfo.EventLeave
		}
		if a, b := eventsOfFabric(n, first, leave), eventsOfFabric(n, second, leave); a != 1 || b != 1 {
			t.Fatalf("fabric-scoped events %d, %d, want 1 each", a, b)
		}
		if _, err := fabrictest.Invoke(ctx, p, cmdPath(0, opCredsClusterID, opCredsCmdRemoveFabric), removeFabricFields{first}); err != nil {
			t.Logf("RemoveFabric answered %v", err)
		}
		if err := site.Eventually(ctx, "the first fabric removed", func() bool {
			f, err := n.Fabrics(ctx)
			return err == nil && len(f) == 1 && f[0].FabricIndex == second
		}); err != nil {
			t.Fatal(err)
		}
		if aclOf(first) != 0 || aclOf(second) != 1 {
			t.Fatalf("ACL entries %d, %d after the removal, want 0 and 1", aclOf(first), aclOf(second))
		}
		if a, b := eventsOfFabric(n, first, leave), eventsOfFabric(n, second, leave); a != 0 || b != 1 {
			t.Fatalf("fabric-scoped events %d, %d after the removal, want 0 and 1", a, b)
		}
	})

	t.Run("removes a fabric while another session holds the fail-safe", func(t *testing.T) {
		t.Parallel()
		t.Run("stays commissioned when another fabric remains", func(t *testing.T) {
			t.Parallel()
			site := quietSite(t)
			p := site.CommissionedPair(fabrictest.PairOptions{})
			p2 := secondFabric(t, site, p)
			ctx := parityCtx(t)
			// The second fabric's session arms the fail-safe
			// (FAILSAFE_LENGTH_S, node-helpers.ts:104).
			if _, err := fabrictest.Invoke(ctx, p2, cmdPath(0, gencommClusterID, 0x00), armFailSafeFields{60, 1}); err != nil {
				t.Fatal(err)
			}
			first := fabricIndexOf(t, p)
			if _, err := fabrictest.Invoke(ctx, p, cmdPath(0, opCredsClusterID, opCredsCmdRemoveFabric), removeFabricFields{first}); err != nil {
				t.Logf("RemoveFabric answered %v", err)
			}
			if err := site.Eventually(ctx, "the first fabric removed", func() bool {
				f, err := p.Device.Node().Fabrics(ctx)
				return err == nil && len(f) == 1
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := fabrictest.Read(ctx, p2, attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)); err != nil {
				t.Fatalf("the remaining fabric reads: %v", err)
			}
		})
	})

	t.Run("restores persisted attribute values after restart", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		ctx := parityCtx(t)
		label := attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
		if err := fabrictest.Write(ctx, p, label, strVal("persisted label")); err != nil {
			t.Fatal(err)
		}
		if err := p.Device.Restart(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := fabrictest.Connect(ctx, p); err != nil {
			t.Fatal(err)
		}
		got, err := fabrictest.Read(ctx, p, label)
		if err != nil || got.Value.El.String != "persisted label" {
			t.Fatalf("NodeLabel after the restart %+v, %v", got, err)
		}
	})

	t.Run("properly deploys aggregator", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		light := newLight(t, site, "light:1")
		p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
		ctx := parityCtx(t)
		lightEP, _ := p.Device.EndpointOf("light:1")
		parts := func(ep uint16) []uint64 {
			d, err := fabrictest.Read(ctx, p, attrPath(ep, descriptor.ClusterID, descriptor.AttrPartsList))
			if err != nil {
				t.Fatal(err)
			}
			var out []uint64
			for _, c := range d.Value.Children {
				out = append(out, c.El.Uint)
			}
			return out
		}
		const aggregator = 1
		if got := parts(0); !slices.Equal(got, []uint64{aggregator, uint64(lightEP)}) {
			t.Fatalf("root PartsList %v, want [1 %d]", got, lightEP)
		}
		if got := parts(aggregator); !slices.Equal(got, []uint64{uint64(lightEP)}) {
			t.Fatalf("Aggregator PartsList %v, want [%d]", got, lightEP)
		}
		d, err := fabrictest.Read(ctx, p, attrPath(lightEP, descriptor.ClusterID, descriptor.AttrServerList))
		if err != nil {
			t.Fatal(err)
		}
		var servers []uint64
		for _, c := range d.Value.Children {
			servers = append(servers, c.El.Uint)
		}
		// matter.js: [3, 4, 6, 98, 29]; the bridged light adds
		// BridgedDeviceBasicInformation (57).
		for _, want := range []uint64{3, 4, 6, 98, 29, 57} {
			if !slices.Contains(servers, want) {
				t.Fatalf("light ServerList %v lacks %d", servers, want)
			}
		}
		if len(servers) != 6 {
			t.Fatalf("light ServerList %v, want the six", servers)
		}
	})
}

// interactionRead is a read request of paths, unfiltered when all.
func interactionRead(all bool, paths ...im.ConcreteAttributePath) interaction.ReadRequest {
	return interaction.ReadRequest{Attributes: paths, NoFabricFilter: all}
}
