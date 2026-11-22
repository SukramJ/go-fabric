// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge_test

// Black-box tests for Bridge hook setters / emitters and attachment methods:
// SetOnReassembled,
// SetOnFabricRemoved / EmitFabricRemoved, AttachCommissioningWindow /
// CommissioningWindow, AttachRootClusters / AttachRootPartsListProvider,
// AttachAggregatorClusters / AttachAggregatorPartsListProvider.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bridge"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/mdns"
)

// ─── SetOnFabricRemoved / EmitFabricRemoved ──────────────────────────────────

func TestBridge_EmitFabricRemoved_CallsHook(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	var got atomic.Uint32
	b.SetOnFabricRemoved(func(idx uint8) { got.Store(uint32(idx)) })
	b.EmitFabricRemoved(5)
	if got.Load() != 5 {
		t.Errorf("EmitFabricRemoved(5): hook received %d", got.Load())
	}
}

func TestBridge_EmitFabricRemoved_NilHook_NoPanic(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	b.SetOnFabricRemoved(nil)
	b.EmitFabricRemoved(2) // must not panic
}

// ─── SetOnReassembled ────────────────────────────────────────────────────────

func TestBridge_SetOnReassembled_CalledAfterReassemble(t *testing.T) {
	t.Parallel()
	b, err := bridge.New(
		emptySnapshotter,
		mdns.NewNoop(),
		bridge.Config{
			Listen: "127.0.0.1:0",
		},
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})

	var called atomic.Bool
	b.SetOnReassembled(func(_ int) { called.Store(true) })
	if err := b.Reassemble(ctx); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if !called.Load() {
		t.Error("SetOnReassembled hook was not called after Reassemble")
	}
}

func TestBridge_SetOnReassembled_NilClearsHook(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})

	var called atomic.Bool
	b.SetOnReassembled(func(_ int) { called.Store(true) })
	b.SetOnReassembled(nil) // clear
	if err := b.Reassemble(ctx); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if called.Load() {
		t.Error("hook should not fire after SetOnReassembled(nil)")
	}
}

// ─── AttachCommissioningWindow / CommissioningWindow ─────────────────────────

func TestBridge_CommissioningWindow_RoundTrip(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	if b.CommissioningWindow() != nil {
		t.Fatal("CommissioningWindow() should be nil before attach")
	}
	w := bridge.NewCommissioningWindow()
	b.AttachCommissioningWindow(w)
	if got := b.CommissioningWindow(); got != w {
		t.Errorf("CommissioningWindow(): want %p, got %p", w, got)
	}
}

func TestBridge_AttachCommissioningWindow_Nil_Detaches(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	w := bridge.NewCommissioningWindow()
	b.AttachCommissioningWindow(w)
	b.AttachCommissioningWindow(nil)
	if got := b.CommissioningWindow(); got != nil {
		t.Errorf("after nil attach, CommissioningWindow should be nil, got %p", got)
	}
}

// ─── AttachRootClusters / AttachRootPartsListProvider ────────────────────────

// fakeMatterClusterServer is a minimal implementation for Attach* tests.
type fakeMatterClusterServer struct {
	id uint32
}

func (f *fakeMatterClusterServer) MatterClusterID() uint32 { return f.id }

func (f *fakeMatterClusterServer) MatterRead(_ uint32) (any, bool) {
	return nil, false
}

func (f *fakeMatterClusterServer) MatterWrite(_ context.Context, _ uint32, _ any) error {
	return nil
}

func (f *fakeMatterClusterServer) MatterInvoke(_ context.Context, _ uint32, _ any) (any, error) {
	return nil, nil
}

func (f *fakeMatterClusterServer) MatterReportable() []uint32 { return nil }

// fakeDescriptor adds SetPartsListProvider to satisfy PartsListProviderSetter.
type fakeDescriptor struct {
	fakeMatterClusterServer
	setProviderCalled bool
	currentProvider   func() []uint16
}

func (d *fakeDescriptor) SetPartsListProvider(fn func() []uint16) {
	d.setProviderCalled = true
	d.currentProvider = fn
}

func TestBridge_AttachRootClusters_NoDescriptor_ReturnsFalse(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	c := &fakeMatterClusterServer{id: 0x0006} // OnOff, not Descriptor
	b.AttachRootClusters([]contract.ClusterServer{c})
	if b.AttachRootPartsListProvider(func() []uint16 { return nil }) {
		t.Error("AttachRootPartsListProvider returned true but no Descriptor cluster is mounted")
	}
}

func TestBridge_AttachRootClusters_WithDescriptor_ReturnsTrue(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	desc := &fakeDescriptor{
		id: 0x001D,
	}
	b.AttachRootClusters([]contract.ClusterServer{desc})
	if !b.AttachRootPartsListProvider(func() []uint16 { return []uint16{2, 3} }) {
		t.Error("AttachRootPartsListProvider returned false but Descriptor cluster is mounted")
	}
	if !desc.setProviderCalled {
		t.Error("SetPartsListProvider was not called on the Descriptor cluster")
	}
}

func TestBridge_AttachAggregatorClusters_NoDescriptor_ReturnsFalse(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	c := &fakeMatterClusterServer{id: 0x0003} // Identify
	b.AttachAggregatorClusters([]contract.ClusterServer{c})
	if b.AttachAggregatorPartsListProvider(func() []uint16 { return nil }) {
		t.Error("AttachAggregatorPartsListProvider returned true but no Descriptor is mounted")
	}
}

func TestBridge_AttachAggregatorClusters_WithDescriptor_ReturnsTrue(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	desc := &fakeDescriptor{
		id: 0x001D,
	}
	b.AttachAggregatorClusters([]contract.ClusterServer{desc})
	if !b.AttachAggregatorPartsListProvider(func() []uint16 { return []uint16{2} }) {
		t.Error("AttachAggregatorPartsListProvider returned false but Descriptor is mounted")
	}
	if !desc.setProviderCalled {
		t.Error("SetPartsListProvider was not called on the aggregator Descriptor")
	}
}

func TestBridge_AttachRootClusters_NilClearsServers(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	desc := &fakeDescriptor{
		id: 0x001D,
	}
	b.AttachRootClusters([]contract.ClusterServer{desc})
	b.AttachRootClusters(nil)
	// After clearing, no Descriptor should be reachable.
	if b.AttachRootPartsListProvider(func() []uint16 { return nil }) {
		t.Error("AttachRootPartsListProvider should return false after clearing root clusters")
	}
}

// TestBridge_StartInstallsTheLivePartsLists (R2-P5-apinode-13): a bridge
// started without either Attach…PartsListProvider serves the live
// PartsList on both Descriptors — the root's lists every endpoint but
// itself, the Aggregator's every bridged one — and a provider the host
// attached before Start is kept.
func TestBridge_StartInstallsTheLivePartsLists(t *testing.T) {
	t.Parallel()
	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		b := newTestBridge(t)
		root, agg := &fakeDescriptor{id: 0x001D}, &fakeDescriptor{id: 0x001D}
		b.AttachRootClusters([]contract.ClusterServer{root})
		b.AttachAggregatorClusters([]contract.ClusterServer{agg})
		if err := b.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = b.Stop(context.Background()) })
		if root.currentProvider == nil || agg.currentProvider == nil {
			t.Fatalf("Start left a Descriptor without a provider: root %v, aggregator %v", root.currentProvider != nil, agg.currentProvider != nil)
		}
		var want []uint16
		for _, ep := range b.Topology().Endpoints {
			if ep != nil && ep.ID != 0 {
				want = append(want, ep.ID)
			}
		}
		if got := root.currentProvider(); len(got) == 0 || len(got) != len(want) {
			t.Errorf("root PartsList %v, want the live non-root endpoints %v", got, want)
		}
		for _, id := range agg.currentProvider() {
			if id == 0 || id == 1 {
				t.Errorf("the Aggregator's PartsList names endpoint %d", id)
			}
		}
	})
	t.Run("host override kept", func(t *testing.T) {
		t.Parallel()
		b := newTestBridge(t)
		root := &fakeDescriptor{id: 0x001D}
		b.AttachRootClusters([]contract.ClusterServer{root})
		if !b.AttachRootPartsListProvider(func() []uint16 { return []uint16{42} }) {
			t.Fatal("AttachRootPartsListProvider refused a mounted Descriptor")
		}
		if err := b.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = b.Stop(context.Background()) })
		if got := root.currentProvider(); len(got) != 1 || got[0] != 42 {
			t.Errorf("root PartsList %v after Start, want the host's [42]", got)
		}
	})
}
