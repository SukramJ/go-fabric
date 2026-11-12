// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// resetStores are a device's stores, held by the test so it can look into
// them across the node the factory reset ends and the one that follows.
type resetStores struct {
	credentials *memstore.Store
	endpoints   *epmem.Store
}

// staleUniqueID is the UniqueID the store holds before the reset, the
// value the node to be reset serves (node-helpers.ts:135-138 sets a known
// uniqueId so the reset's effect is visible); after the reset the store
// holds another one or none (:164-165).
const staleUniqueID = "asdf"

func newResetStores(t *testing.T) resetStores {
	t.Helper()
	s := resetStores{credentials: memstore.New(), endpoints: epmem.New()}
	if err := s.credentials.SetSetting(t.Context(), store.SettingUniqueID, staleUniqueID); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s resetStores) configure(cfg *node.Config) {
	cfg.Store = node.Store{Credentials: s.credentials, Endpoints: s.endpoints}
}

// assertFactoryState holds testFactoryReset's checks (matter.js
// packages/node/src/testing/node-helpers.ts:112-175 at 862ad75f) on the Go
// node: no fabric is left, the UniqueID the old node served did not
// persist (the store holds none, or the one a node that came back pinned),
// the device's BasicInformation and pairing codes are there, and the
// device runs exactly when want says so.
func assertFactoryState(t *testing.T, d *fabrictest.Device, s resetStores, wantRunning bool) {
	t.Helper()
	ctx := parityCtx(t)
	if got := d.Running(); got != wantRunning {
		t.Fatalf("device running %v after the factory reset, want %v", got, wantRunning)
	}
	if !wantRunning {
		// The host comes back as matter.js's offline node is: unstarted
		// until it is started, on the erased store.
		fabrics, err := s.credentials.ListFabrics(ctx)
		if err != nil || len(fabrics) != 0 {
			t.Fatalf("fabrics after the factory reset %v (%v), want none", fabrics, err)
		}
		if v, ok, _ := s.credentials.GetSetting(ctx, store.SettingUniqueID); ok && v == staleUniqueID {
			t.Fatalf("UniqueID %q persisted across the factory reset", v)
		}
		if err := d.Start(ctx); err != nil {
			t.Fatal(err)
		}
	}
	n := d.Node()
	if infos, err := n.FabricInfos(ctx); err != nil || len(infos) != 0 {
		t.Fatalf("fabrics of the node after the factory reset %v (%v), want none", infos, err)
	}
	if v, ok := n.Root().BasicInformation.MatterRead(0x0001); !ok || !strings.Contains(fmt.Sprint(v), "go-fabric") { // VendorName
		t.Fatalf("VendorName %v (%v), want go-fabric", v, ok)
	}
	payload, err := n.SetupPayload()
	if err != nil || payload.Manual == "" {
		t.Fatalf("pairing codes after the factory reset %+v (%v)", payload, err)
	}
	if !n.Window().Open {
		t.Fatal("the uncommissioned node opened no commissioning window")
	}
}

// TestServerNodeFactoryResetParityMatterJS ports the factory-reset cases of
// matter.js packages/node/test/node/ServerNodeTest.ts at 862ad75f. A Go
// node is single-use: node.FactoryReset removes the fabrics through the
// RemoveFabric fan-out, stops the node and erases its stores, and the host
// — fabrictest.Device.FactoryReset here — builds the node that comes back
// (BD-Matter-FactoryResetEndsTheNode). matter.js's online/offline
// transitions are the device's running state; its "unique id did not
// persist" is the UniqueID the old node served gone from the store (a
// node that came back before a parallel stop pinned its own).
func TestServerNodeFactoryResetParityMatterJS(t *testing.T) {
	t.Parallel()

	commissioned := func(t *testing.T) (*fabrictest.Site, *fabrictest.Pair, resetStores) {
		t.Helper()
		s := newResetStores(t)
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: s.configure}})
		return site, p, s
	}

	// ServerNodeTest.ts:655-657, testFactoryReset("offline-after-commission").
	t.Run("factory resets when offline after commission", func(t *testing.T) {
		t.Parallel()
		_, p, s := commissioned(t)
		if err := p.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if err := p.Device.FactoryReset(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		assertFactoryState(t, p.Device, s, false)
	})

	// ServerNodeTest.ts:659-661, testFactoryReset("online").
	t.Run("factory resets when online after commission", func(t *testing.T) {
		t.Parallel()
		_, p, s := commissioned(t)
		if before := p.Device.Node().UniqueID(); before != staleUniqueID {
			t.Fatalf("UniqueID %q served before the reset, want the stored %q", before, staleUniqueID)
		}
		if err := p.Device.FactoryReset(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		assertFactoryState(t, p.Device, s, true)
		// node-helpers.ts:164-165: the node that came back serves its own.
		got := p.Device.Node().UniqueID()
		if got == "" || got == staleUniqueID {
			t.Fatalf("UniqueID %q served after the reset", got)
		}
		// A fresh one, drawn at random, not derived again: a second reset
		// of the same identity rotates it once more.
		if err := p.Device.FactoryReset(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if again := p.Device.Node().UniqueID(); again == "" || again == got {
			t.Fatalf("UniqueID %q served after the second reset, want another than %q", again, got)
		}
	})

	// ServerNodeTest.ts:663-665, testFactoryReset("offline"): a node that
	// was never commissioned and is offline.
	t.Run("factory resets when offline without commission", func(t *testing.T) {
		t.Parallel()
		s := newResetStores(t)
		site := quietSite(t)
		u := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: s.configure}})
		if err := u.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if err := u.Device.FactoryReset(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		assertFactoryState(t, u.Device, s, false)
	})

	// ServerNodeTest.ts:667-669, testFactoryReset("offline-during-reset"):
	// a stop called while the reset runs leaves the device offline.
	t.Run("handles factory resets when online but in parallel offline is called correctly", func(t *testing.T) {
		t.Parallel()
		_, p, s := commissioned(t)
		ctx := parityCtx(t)
		var wg sync.WaitGroup
		var resetErr, stopErr error
		wg.Add(2)
		go func() { defer wg.Done(); resetErr = p.Device.FactoryReset(ctx) }()
		go func() { defer wg.Done(); stopErr = p.Device.Stop(ctx) }()
		wg.Wait()
		if err := errors.Join(resetErr, stopErr); err != nil {
			t.Fatal(err)
		}
		if p.Device.Running() {
			// The reset came back first and the stop took it down after,
			// or the stop came first: either way the device ends offline.
			t.Fatal("the device runs after a stop called during its factory reset")
		}
		assertFactoryState(t, p.Device, s, false)
	})

	// ServerNodeTest.ts:699-719: after a factory reset and a second
	// commissioning, the removal of the fabric runs its fan-out once (Go's
	// sanitisation is that fan-out; the host hears it as OnFabricRemoved).
	t.Run("sanitizes fabric-scoped data once per fabric removal after a factory reset", func(t *testing.T) {
		t.Parallel()
		site, p, _ := commissioned(t)
		ctx := parityCtx(t)
		if err := p.Device.FactoryReset(ctx); err != nil {
			t.Fatal(err)
		}
		again, err := p.Controller.Commission(ctx, p.Device, fabrictest.CommissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		n := again.Device.Node()
		var removed atomic.Int32
		unsubscribe := n.Observe(node.Observers{OnFabricRemoved: func(node.FabricInfo) { removed.Add(1) }})
		defer unsubscribe()
		infos, err := n.FabricInfos(ctx)
		if err != nil || len(infos) != 1 {
			t.Fatalf("fabrics after the second commissioning %v (%v), want one", infos, err)
		}
		if err := site.Run(ctx, func(ctx context.Context) error {
			return again.Device.Act(func(n *node.Node) error { return n.RemoveFabric(ctx, infos[0].FabricIndex) })
		}); err != nil {
			t.Fatal(err)
		}
		if err := site.Eventually(ctx, "the fabric removal", func() bool { return removed.Load() >= 1 }); err != nil {
			t.Fatal(err)
		}
		if err := site.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		if got := removed.Load(); got != 1 {
			t.Fatalf("fabric removal ran %d times, want once", got)
		}
	})

	// ServerNodeTest.ts:850-869: a factory reset frees the number a stored
	// endpoint that is not present holds. matter.js presets the numbers and
	// reboots with the first endpoint only; Go has no presets, so the
	// second light is added and removed while the node runs — its record,
	// and with it its number, stays held (the assembler never removes a
	// record, endpoint/assembler.go) — until the factory reset erases it.
	t.Run("frees the endpoint numbers a factory reset erases", func(t *testing.T) {
		t.Parallel()
		s := newResetStores(t)
		site := quietSite(t)
		first := newLight(t, site, "first")
		d := site.AddDevice(fabrictest.Topology{Configure: s.configure, Bridged: []node.Endpoint{first}})
		ctx := parityCtx(t)
		second := newLight(t, site, "second")
		if err := d.Act(func(n *node.Node) error { return n.Aggregator().Add(ctx, second) }); err != nil {
			t.Fatal(err)
		}
		if err := d.Act(func(n *node.Node) error { return n.Aggregator().Remove(ctx, endpoint.StringKey("second")) }); err != nil {
			t.Fatal(err)
		}
		if _, err := s.endpoints.GetEndpoint(ctx, endpoint.StringKey("second")); err != nil {
			t.Fatalf("the removed endpoint's record before the reset: %v, want it held", err)
		}
		if err := d.FactoryReset(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := s.endpoints.GetEndpoint(ctx, endpoint.StringKey("second")); !errors.Is(err, endpoint.ErrNotFound) {
			t.Fatalf("the removed endpoint's record after the reset: %v, want ErrNotFound", err)
		}
		if _, ok := d.EndpointOf("first"); !ok {
			t.Fatal("the present endpoint has no number after the reset")
		}
	})

	// Go's own case (matter.js has no erase-then-assign test at the pin;
	// EndpointNumberConflictTest.ts covers conflicts only): the reset
	// restarts the numbering, as ServerEndpointStores.ts erase (:63-77) sets
	// #nextNumber back, so the node that comes back on the erased store
	// numbers the same sources from 2 as a fresh install — not from the
	// old high-water mark.
	t.Run("restarts the endpoint numbering after a factory reset", func(t *testing.T) {
		t.Parallel()
		s := newResetStores(t)
		site := quietSite(t)
		keys := []string{"first", "second", "third"}
		lights := make([]node.Endpoint, 0, len(keys))
		for _, k := range keys {
			lights = append(lights, newLight(t, site, k))
		}
		p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: s.configure, Bridged: lights}})
		ctx := parityCtx(t)
		partsList := client.AttributeAt(1, 0x001D, 0x0003)
		parts := func(p *fabrictest.Pair) []uint64 {
			t.Helper()
			got, err := fabrictest.Read(ctx, p, partsList)
			if err != nil {
				t.Fatal(err)
			}
			listed := make([]uint64, 0, len(got.Value.Children))
			for _, c := range got.Value.Children {
				listed = append(listed, c.El.Uint)
			}
			slices.Sort(listed)
			return listed
		}
		want := []uint64{2, 3, 4}
		if got := parts(p); !slices.Equal(got, want) {
			t.Fatalf("PartsList before the reset %v, want %v", got, want)
		}
		// A removed source lifts the high-water mark past every number
		// the topology holds.
		extra := newLight(t, site, "extra")
		if err := p.Device.Act(func(n *node.Node) error { return n.Aggregator().Add(ctx, extra) }); err != nil {
			t.Fatal(err)
		}
		if err := p.Device.Act(func(n *node.Node) error { return n.Aggregator().Remove(ctx, endpoint.StringKey("extra")) }); err != nil {
			t.Fatal(err)
		}
		if err := p.Device.FactoryReset(ctx); err != nil {
			t.Fatal(err)
		}
		again, err := p.Controller.Commission(ctx, p.Device, fabrictest.CommissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := parts(again); !slices.Equal(got, want) {
			t.Fatalf("PartsList after the reset %v, want %v (a fresh install's numbering)", got, want)
		}
		for _, k := range keys {
			if id, ok := again.Device.EndpointOf(k); !ok || !slices.Contains(want, uint64(id)) {
				t.Errorf("endpoint of %s after the reset %d (%v), want one of %v", k, id, ok, want)
			}
		}
	})
}
