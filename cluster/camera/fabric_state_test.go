// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	"github.com/SukramJ/go-fabric/im"
)

// TestProviderDropFabricEndsItsSessions: a removed fabric's WebRTC
// sessions end through the port and leave CurrentSessions; another
// fabric's stay (review II, R2-P2-fabric-04).
func TestProviderDropFabricEndsItsSessions(t *testing.T) {
	p, m := newProvider(t, nil)
	for _, s := range []camera.ProviderSession{{Id: 1, PeerNodeId: 7, FabricIndex: 1}, {Id: 2, PeerNodeId: 8, FabricIndex: 2}} {
		if err := p.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	if got := p.MatterHeldFabrics(); !slices.Equal(got, []uint8{1, 2}) {
		t.Fatalf("held %v, want [1 2]", got)
	}
	if err := p.MatterDropFabric(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if got := p.MatterHeldFabrics(); !slices.Equal(got, []uint8{2}) {
		t.Errorf("held after the drop %v, want [2]", got)
	}
	m.mu.Lock()
	ended := slices.Clone(m.ended)
	m.mu.Unlock()
	if !slices.Equal(ended, []uint16{1}) {
		t.Errorf("port ended %v, want [1]", ended)
	}
}

// TestRequestorDropFabricStripsItsSessions: a removed fabric's sessions
// leave CurrentSessions, as matter.js strips a fabric-sensitive list on
// the fabric's deletion, and the port's End is told of each.
func TestRequestorDropFabricStripsItsSessions(t *testing.T) {
	rec := &recorder{}
	r := newRequestor(t, rec)
	upsert(t, r, makeSession(withID(1), withFabric(1)), makeSession(withID(2), withFabric(2), withPeer(9)))
	if got := r.MatterHeldFabrics(); !slices.Equal(got, []uint8{1, 2}) {
		t.Fatalf("held %v, want [1 2]", got)
	}
	if err := r.MatterDropFabric(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if got := fabrics(r.Sessions()); !slices.Equal(got, []uint8{2}) {
		t.Errorf("sessions' fabrics after the drop %v, want [2]", got)
	}
	rec.mu.Lock()
	ends := fabrics(rec.ends)
	rec.mu.Unlock()
	if !slices.Equal(ends, []uint8{1}) {
		t.Errorf("End told for fabrics %v, want [1]", ends)
	}
}

// TestPushDropFabricDeallocatesItsConnections: a removed fabric's push
// transports are deallocated through the port and leave
// CurrentConnections; another fabric's stay.
func TestPushDropFabricDeallocatesItsConnections(t *testing.T) {
	p, m := newPush(t)
	own := allocatePush(t, p, motionOptions())
	if _, err := p.MatterInvoke(peer(2, 0x56), padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: motionOptions()}); statusOf(err) != im.StatusSuccess {
		t.Fatalf("allocate as fabric 2: %v", err)
	}
	if got := p.MatterHeldFabrics(); !slices.Equal(got, []uint8{1, 2}) {
		t.Fatalf("held %v, want [1 2]", got)
	}
	if err := p.MatterDropFabric(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if got := p.MatterHeldFabrics(); !slices.Equal(got, []uint8{2}) {
		t.Errorf("held after the drop %v, want [2]", got)
	}
	m.mu.Lock()
	deallocated := slices.Clone(m.deallocated)
	m.mu.Unlock()
	if !slices.Equal(deallocated, []uint16{own.ConnectionId}) {
		t.Errorf("port deallocated %v, want [%d]", deallocated, own.ConnectionId)
	}
}
