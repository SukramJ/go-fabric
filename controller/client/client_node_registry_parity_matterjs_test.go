// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	powersourcedef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// A port of matter.js packages/node/test/node/ClientNodeTest.ts at
// 862ad75f, the registry cases phase 32 built the features for: a
// commission with an explicit local id (:505-535), the scanners told to
// forget a commissioned device (:777-938), the fabricIndexOnPeer backfill
// on reload (:2867-2884) and the version filters of a peer with a
// PowerSource on its root (:3192-3223). matter.js's ScannerSet is the commissioner's
// Scanners (fabrictest.ControllerOptions.Scanners), its
// peers.commission({ id }) the registry's ForDevice with an id.

// forgetRecorder is ForgetRecordingScanner: it records what it is told
// to forget.
type forgetRecorder struct {
	mu         sync.Mutex
	identities []*discovery.CommissionedIdentity
	calls      int
}

func (r *forgetRecorder) ForgetCommissioned(_ []string, identity *discovery.CommissionedIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.identities = append(r.identities, identity)
}

func (r *forgetRecorder) seen() (calls int, identities []*discovery.CommissionedIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, slices.Clone(r.identities)
}

// forgetFailing is ForgetFailingScanner: its bookkeeping fails.
type forgetFailing struct{}

func (forgetFailing) ForgetCommissioned([]string, *discovery.CommissionedIdentity) {
	panic("scanner bookkeeping failed")
}

// commissionAs discovers the pair's device and commissions it through the
// registry, the new node taking id ("" allocates one).
func commissionAs(t *testing.T, site *fabrictest.Site, pair *fabrictest.Pair, id string, o client.CommissionOptions) (*client.Node, error) {
	t.Helper()
	found, err := pair.Controller.Discover(parityCtx(t), discovery.ByLongDiscriminator(pair.Device.Discriminator()), 0)
	if err != nil || len(found) == 0 {
		t.Fatalf("discover: %v (%d found)", err, len(found))
	}
	reg := pair.Controller.Registry()
	n, err := reg.ForDevice(parityCtx(t), found[0], id)
	if err != nil {
		t.Fatal(err)
	}
	if o.Passcode == 0 {
		o.Passcode = pair.Device.Passcode()
	}
	o.AutoSubscribe = boolPtr(false)
	return n, site.Run(parityCtx(t), func(ctx context.Context) error { return reg.Commission(ctx, n, o) })
}

func TestClientNodeRegistryParity(t *testing.T) {
	t.Parallel()

	t.Run("commissions with an explicit id and restores the peer under that id after restart", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		pair := site.UncommissionedPair(fabrictest.PairOptions{})
		if _, err := commissionAs(t, site, pair, "device", client.CommissionOptions{}); err != nil {
			t.Fatal(err)
		}
		ids := func() []string {
			nodes := pair.Controller.Registry().Nodes()
			out := make([]string, 0, len(nodes))
			for _, n := range nodes {
				out = append(out, n.ID())
			}
			return out
		}
		if got := ids(); !slices.Equal(got, []string{"device"}) {
			t.Fatalf("nodes %v, want [device]", got)
		}
		if err := pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if got := ids(); !slices.Equal(got, []string{"device"}) {
			t.Fatalf("nodes after the restart %v, want [device]", got)
		}
		if pair.Controller.Registry().Get("device") == nil {
			t.Fatal("the node is not restored under its id")
		}
	})

	scannerPair := func(t *testing.T, scanners ...commissioner.CommissionedScanner) (*fabrictest.Site, *fabrictest.Pair) {
		t.Helper()
		site := quietSite(t)
		pair := site.UncommissionedPair(fabrictest.PairOptions{Controller: fabrictest.ControllerOptions{Scanners: scanners}})
		return site, pair
	}

	t.Run("tells the scanners to forget a device it commissioned", func(t *testing.T) {
		t.Parallel()
		rec := &forgetRecorder{}
		site, pair := scannerPair(t, rec)
		if _, err := commissionAs(t, site, pair, "", client.CommissionOptions{}); err != nil {
			t.Fatal(err)
		}
		if calls, _ := rec.seen(); calls != 1 {
			t.Fatalf("the scanner was told %d times, want once", calls)
		}
	})

	t.Run("tells the scanners the identity of a device it discovered and commissioned", func(t *testing.T) {
		t.Parallel()
		rec := &forgetRecorder{}
		site, pair := scannerPair(t, rec)
		n, err := commissionAs(t, site, pair, "", client.CommissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_, ids := rec.seen()
		r := n.Record()
		if r.VendorID == nil || r.ProductID == nil {
			t.Fatal("the record has no vendor and product")
		}
		want := discovery.CommissionedIdentity{D: pair.Device.Discriminator(), VP: fmt.Sprintf("%d+%d", *r.VendorID, *r.ProductID)}
		if len(ids) != 1 || ids[0] == nil || *ids[0] != want {
			t.Fatalf("identities %+v, want [%+v]", ids, want)
		}
	})

	t.Run("leaves a device the scanners know when commissioning it fails", func(t *testing.T) {
		t.Parallel()
		rec := &forgetRecorder{}
		site, pair := scannerPair(t, rec)
		if _, err := commissionAs(t, site, pair, "", client.CommissionOptions{Options: commissioner.Options{Passcode: 11112222}}); err == nil {
			t.Fatal("a wrong passcode commissioned")
		}
		if calls, _ := rec.seen(); calls != 0 {
			t.Fatalf("the scanner was told %d times, want never", calls)
		}
	})

	t.Run("leaves a device the scanners know when the commissioning steps fail", func(t *testing.T) {
		t.Parallel()
		rec := &forgetRecorder{}
		site, pair := scannerPair(t, rec)
		failing := errors.New("commissioning step failed")
		o := client.CommissionOptions{Options: commissioner.Options{
			FinalizeCommissioning: func(context.Context, handshake.PeerAddress, *discovery.Device, *sessionparams.Parameters) error {
				return failing
			},
		}}
		if _, err := commissionAs(t, site, pair, "", o); !errors.Is(err, failing) {
			t.Fatalf("commission: %v, want the failed step", err)
		}
		if calls, _ := rec.seen(); calls != 0 {
			t.Fatalf("the scanner was told %d times, want never", calls)
		}
	})

	t.Run("commissions a device and tells the other scanners when one scanner fails to forget it", func(t *testing.T) {
		t.Parallel()
		rec := &forgetRecorder{}
		site, pair := scannerPair(t, forgetFailing{}, rec)
		if _, err := commissionAs(t, site, pair, "", client.CommissionOptions{}); err != nil {
			t.Fatal(err)
		}
		if calls, _ := rec.seen(); calls != 1 {
			t.Fatalf("the other scanner was told %d times, want once", calls)
		}
	})

	t.Run("backfills fabricIndexOnPeer from operational credentials on reload when state was cleared", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{commission: client.CommissionOptions{AutoSubscribe: boolPtr(false)}})
		captured := cp.node.Record().FabricIndexOnPeer
		if captured == 0 {
			t.Fatal("no fabric index captured at commissioning")
		}
		client.UpdateRecord(cp.node, func(r *client.Record) { r.FabricIndexOnPeer = 0 })
		if cp.node.Record().FabricIndexOnPeer != 0 {
			t.Fatal("the fabric index was not cleared")
		}
		id := cp.node.ID()
		if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		n := cp.pair.Controller.Registry().Get(id)
		if n == nil {
			t.Fatalf("node %q not restored", id)
		}
		if got := n.Record().FabricIndexOnPeer; got != captured {
			t.Fatalf("fabricIndexOnPeer %d after the reload, want %d", got, captured)
		}
	})

	t.Run("omits version filters only for clusters in which the peer has a changes-omitted attribute", func(t *testing.T) {
		t.Parallel()
		battery, err := spec.NewServer(powersourcedef.Definition, spec.Options{Features: uint32(powersourcedef.FeatureBattery)}, spec.ServerConfig{
			Initial: powersourcedef.Patch{
				Status: new(powersourcedef.PowerSourceStatusActive), Order: new(uint8(0)), Description: new("Battery"),
				BatChargeLevel: new(powersourcedef.BatChargeLevelOk), BatReplacementNeeded: new(false),
				BatReplaceability: new(powersourcedef.BatReplaceabilityUnspecified),
			}.AttributeValues(),
		})
		if err != nil {
			t.Fatal(err)
		}
		cp := newClientPair(t, clientPairOptions{
			rootOnly:  true,
			configure: func(c *node.Config) { c.RootServers = []contract.ClusterServer{battery} },
		})
		read := interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}, NoFabricFilter: !client.CacheFabricFiltered(cp.node)}
		on0 := func(filters []im.DataVersionFilter) []uint32 {
			var out []uint32
			for _, f := range filters {
				if f.Endpoint == 0 {
					out = append(out, f.Cluster)
				}
			}
			return out
		}
		plain := on0(client.KnownVersions(cp.node, read))
		refreshing := on0(client.KnownReadVersions(cp.node, read))
		for _, c := range []uint32{generaldiagnostics.ClusterID, accessControlCluster} {
			if !slices.Contains(plain, c) {
				t.Fatalf("filters %v lack cluster 0x%04x", plain, c)
			}
		}
		if slices.Contains(refreshing, generaldiagnostics.ClusterID) {
			t.Fatal("a read refreshing the changes-omitted attributes filters GeneralDiagnostics")
		}
		if !slices.Contains(refreshing, powersourcedef.ClusterID) {
			t.Fatalf("filters %v lack the root's PowerSource", refreshing)
		}
	})
}
