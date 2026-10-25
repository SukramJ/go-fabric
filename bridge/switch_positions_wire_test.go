// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"

	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/store"
)

// positionSource is a host's push button that knows its position, not the
// gestures a press makes: a MomentarySwitch measurement source with
// multi-press recognition, handed the Switch server's position input at
// reassembly (WireMatterSwitchPositions).
type positionSource struct {
	mu   sync.Mutex
	sink contract.SwitchPositionSink
}

func (*positionSource) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementMomentarySwitch
}
func (*positionSource) MatterSwitchPositions() uint8        { return 2 }
func (*positionSource) MatterSwitchSupportsLongPress() bool { return true }
func (*positionSource) MatterSwitchMultiPressMax() uint8    { return 3 }

func (p *positionSource) WireMatterSwitchPositions(sink contract.SwitchPositionSink) func() {
	p.mu.Lock()
	p.sink = sink
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		if p.sink == sink {
			p.sink = nil
		}
		p.mu.Unlock()
	}
}

var (
	_ clusterwire.GenericSwitchSource           = (*positionSource)(nil)
	_ clusterwire.GenericSwitchMultiPressSource = (*positionSource)(nil)
)

// TestSwitchPositionsReachTheWire: the bridge hands a position source the
// Switch server's position input, and a press reported as positions comes
// out as the server's decided events — InitialPress and ShortRelease for a
// short press of a switch with MS, MSR, MSL and MSM (matter.js
// SwitchServer.ts:253-301), then MultiPressComplete once the multi-press
// delay passed (:357-370) — readable over the wire, on the source's
// endpoint.
func TestSwitchPositionsReachTheWire(t *testing.T) {
	t.Parallel()
	src := &positionSource{}
	h := newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
		asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
		if err != nil {
			t.Fatalf("endpoint.New: %v", err)
		}
		spec := endpoint.Spec{StableKey: endpoint.StringKey("button"), DeviceType: 0x000F, FriendlyName: "Button", Measurement: src}
		return func(ctx context.Context) (*endpoint.Topology, error) {
			return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "app", Endpoints: []endpoint.Spec{spec}}})
		}, nil
	})
	h.allowAll()
	bridged := h.bridge.Topology().Bridged()
	if len(bridged) != 1 {
		t.Fatalf("assembled %d bridged endpoints, want 1", len(bridged))
	}
	src.mu.Lock()
	sink := src.sink
	src.mu.Unlock()
	if sink == nil {
		t.Fatal("the bridge did not hand the source the position input")
	}
	if err := sink.SetCurrentPosition(1); err != nil {
		t.Fatal(err)
	}
	if err := sink.SetCurrentPosition(0); err != nil {
		t.Fatal(err)
	}
	if err := sink.SetCurrentPosition(2); err == nil {
		t.Error("position 2 of a two-position switch was accepted")
	}
	events := h.readEvents(bridged[0].ID, switchClusterID)
	ids := make([]uint64, 0, len(events))
	for _, ev := range events {
		ids = append(ids, ev.id)
	}
	// The server runs on the system clock here: MultiPressComplete (0x06)
	// follows once the 300 ms multi-press delay has passed, which a slow
	// run may reach before the read.
	if !slices.Equal(ids, []uint64{0x01, 0x03}) && !slices.Equal(ids, []uint64{0x01, 0x03, 0x06}) {
		t.Fatalf("Switch events %v, want InitialPress, ShortRelease (and MultiPressComplete)", ids)
	}
}
