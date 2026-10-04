// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"sync"
	"testing"

	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/store"
)

// pressSource is a host's momentary push button: a MomentarySwitch
// measurement source the measurement materializer turns into the
// GenericSwitch server, and the press hook the bridge wires at
// reassembly (WireMatterSwitchHandler).
type pressSource struct {
	mu      sync.Mutex
	emitter contract.SwitchEventEmitter
}

func (*pressSource) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementMomentarySwitch
}
func (*pressSource) MatterSwitchPositions() uint8        { return 2 }
func (*pressSource) MatterSwitchSupportsLongPress() bool { return true }

func (p *pressSource) WireMatterSwitchHandler(e contract.SwitchEventEmitter) func() {
	p.mu.Lock()
	p.emitter = e
	p.mu.Unlock()
	return func() {}
}

var _ clusterwire.GenericSwitchSource = (*pressSource)(nil)

// TestSwitchPressEventsMatchMatterJSOverTheWire fires the four press
// events through the production path — the bridge's switch hook, the
// event log, an event Read and the value writer — and compares each
// EventDataIB Data element with the bytes matter.js encodes for the
// event (application-wire-fixtures.json, switch_*). Before the payload
// types were exported the Data slot carried TLV null.
func TestSwitchPressEventsMatchMatterJSOverTheWire(t *testing.T) {
	t.Parallel()
	src := &pressSource{}
	h := newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
		asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
		if err != nil {
			t.Fatalf("endpoint.New: %v", err)
		}
		spec := endpoint.Spec{StableKey: endpoint.StringKey("button"), DeviceType: 0x000F, FriendlyName: "Button", Measurement: src}
		return func(ctx context.Context) (*endpoint.Topology, error) {
			return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "app", Endpoints: []endpoint.Spec{spec}, ModelComplete: true}})
		}, nil
	})
	h.allowAll()
	bridged := h.bridge.Topology().Bridged()
	if len(bridged) != 1 {
		t.Fatalf("assembled %d bridged endpoints, want 1", len(bridged))
	}
	src.mu.Lock()
	emitter := src.emitter
	src.mu.Unlock()
	if emitter == nil {
		t.Fatal("the bridge did not wire the switch hook")
	}

	var fixtures []applicationWireFixture
	if err := json.Unmarshal(applicationWireFixturesJSON, &fixtures); err != nil {
		t.Fatal(err)
	}
	want := map[uint64]string{}
	for _, f := range fixtures {
		if f.Cluster != switchClusterID {
			continue
		}
		id := map[string]uint64{"InitialPress": 1, "LongPress": 2, "ShortRelease": 3, "LongRelease": 4}[f.Element]
		want[id] = f.BytesHex
		switch f.Element {
		case "InitialPress":
			emitter.FireInitialPress(f.Fixture.NewPosition)
		case "LongPress":
			emitter.FireLongPress(f.Fixture.NewPosition)
		case "ShortRelease":
			emitter.FireShortRelease(f.Fixture.PreviousPosition)
		case "LongRelease":
			emitter.FireLongRelease(f.Fixture.PreviousPosition)
		}
	}
	if len(want) != 4 {
		t.Fatalf("found %d Switch fixtures, want 4", len(want))
	}
	events := h.readEvents(bridged[0].ID, switchClusterID)
	if len(events) != 4 {
		t.Fatalf("read back %d Switch events, want 4", len(events))
	}
	for _, ev := range events {
		raw, err := hex.DecodeString(want[ev.id])
		if err != nil {
			t.Fatal(err)
		}
		if !sameTLV(ev.data, decodeTLVTree(t, raw)) {
			t.Errorf("event 0x%02X Data = %+v, matter.js %s", ev.id, ev.data, want[ev.id])
		}
	}
}

// sameTLV compares two decoded TLV trees by element type, context tag
// and value — the wire shape, width included, without the outer tag.
func sameTLV(a, b tlvNode) bool {
	if a.El.Type != b.El.Type || a.El.IsNull != b.El.IsNull || a.El.Bool != b.El.Bool || a.El.Uint != b.El.Uint ||
		a.El.Int != b.El.Int || a.El.String != b.El.String || string(a.El.Octets) != string(b.El.Octets) ||
		len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Children {
		if a.Children[i].El.Tag != b.Children[i].El.Tag || !sameTLV(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}
