// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/store"
)

// joiningOnOff is an OnOff server that holds state for the fabrics in
// held ([contract.FabricScopedState]); whenever the node hands it the
// fail-safe port — after the reconcile, before the topology is published —
// it runs during, once.
type joiningOnOff struct {
	*onoff.Server
	mu     sync.Mutex
	held   []uint8
	during func()
}

func (s *joiningOnOff) MatterDropFabric(_ context.Context, fabricIndex uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = slices.DeleteFunc(s.held, func(f uint8) bool { return f == fabricIndex })
	return nil
}

func (s *joiningOnOff) MatterHeldFabrics() []uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.held)
}

func (*joiningOnOff) OnFailSafeExpired(context.Context)       {}
func (*joiningOnOff) OnCommissioningComplete(context.Context) {}

func (s *joiningOnOff) AttachFailSafe(func() (uint8, bool), func(uint64)) {
	s.mu.Lock()
	during := s.during
	s.during = nil
	s.mu.Unlock()
	if during != nil {
		during()
	}
}

type joiningPlug struct{ srv *joiningOnOff }

func (p *joiningPlug) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffPlugInUnit }

func (p *joiningPlug) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p.srv}
}

// TestReconcileCatchesARemovalBeforeThePublish (review II, R2-P2-dr-node-03):
// a fabric removed after the reconcile read the table, but before the
// node published the new topology — so the removal's fan-out walked the
// topology before it — is dropped from the joining endpoint all the same.
func TestReconcileCatchesARemovalBeforeThePublish(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	n := newMDNSTestNode(t, cfg)
	fi, err := n.st.AddFabric(ctx, store.FabricRecord{FabricID: 0x0D03, NodeID: 0x0D04, VendorID: 0xFFF1, RootPublicKey: make([]byte, 65), CompressedID: [8]byte{3}})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	joining := &joiningOnOff{Server: srv, held: []uint8{fi}}
	// The removal as fabricRemoved runs it: the row first, then the
	// fan-out over the topology the node published last.
	joining.during = func() {
		if err := n.st.RemoveFabric(ctx, fi); err != nil {
			t.Error(err)
		}
		n.dropFabricState(ctx, fi)
	}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("plug:joining"), DeviceAddress: "J", DeviceType: onoff.DeviceTypeOnOffPlugInUnit,
			FriendlyName: "Joining", ChannelAddress: "J:1", Source: &joiningPlug{srv: joining},
		}}}})
	}
	if _, err := n.observeTopology(inner)(ctx); err != nil {
		t.Fatalf("observeTopology: %v", err)
	}
	if held := joining.MatterHeldFabrics(); len(held) != 0 {
		t.Fatalf("the joining endpoint still holds fabrics %v after the removal of %d", held, fi)
	}
}
