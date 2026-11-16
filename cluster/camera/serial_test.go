// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// blockFirstChange holds the first change notification of srv until
// release is closed and reports, on entered, that it is held: a change
// still on its way to the store while a second one runs.
func blockFirstChange(srv contract.AttributeChangeNotifier) (entered, release chan struct{}) {
	var first atomic.Bool
	first.Store(true)
	entered, release = make(chan struct{}), make(chan struct{})
	srv.OnAttributeChange(func(contract.Change) {
		if first.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
	})
	return entered, release
}

// settle yields until done holds a value or a bounded number of yields
// passed: a second change that is not serialised behind the held first one
// completes meanwhile, one that is waits — without a wall-clock wait.
func settle[T any](done chan T) {
	for range 10000 {
		if len(done) > 0 {
			return
		}
		runtime.Gosched()
	}
}

// TestProviderChangesDoNotLoseEachOther interleaves two changes of the
// session table: a removal held between its publication and its commit,
// and a new session stored meanwhile. Each change applies to the table
// the one before it left, so the table and CurrentSessions both end with
// session 2 only. Before, each change cloned the table, released the lock
// and stored its copy: the removal's copy overwrote the new session in the
// table while the attribute kept both (review II, R2-P6-conc-1601).
func TestProviderChangesDoNotLoseEachOther(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t, nil)
	video := []uint16{1}
	if err := p.UpsertSession(camera.ProviderSession{Id: 1, FabricIndex: 1, PeerNodeId: 1, VideoStreams: &video}); err != nil {
		t.Fatal(err)
	}
	entered, release := blockFirstChange(p)
	removed := make(chan struct{})
	go func() { p.RemoveSession(1); close(removed) }() // fabric 1's EndSession
	<-entered
	stored := make(chan error, 1)
	go func() { // fabric 2's SolicitOffer, while the removal is on its way
		stored <- p.UpsertSession(camera.ProviderSession{Id: 2, FabricIndex: 2, PeerNodeId: 2, VideoStreams: &video})
	}()
	settle(stored)
	close(release)
	<-removed
	if err := <-stored; err != nil {
		t.Fatal(err)
	}
	if s := p.Sessions(); len(s) != 1 || s[0].Id != 2 {
		t.Errorf("Sessions() = %+v, want session 2 only", s)
	}
	attr, _ := p.MatterRead(wpdef.AttrCurrentSessions)
	if l, ok := attr.(spec.List[camera.ProviderSession]); !ok || len(l) != 1 || l[0].Id != 2 {
		t.Errorf("CurrentSessions = %#v, want session 2 only", attr)
	}
}

// gatedPush is the push port whose AllocatePushTransport waits on gate
// after it reported, on entered, that it was called.
type gatedPush struct {
	*mockPush
	entered, gate chan struct{}
}

func (g gatedPush) AllocatePushTransport(ctx context.Context, id uint16, fabric uint8, o padef.TransportOptionsStruct, c camera.TLSCredentials) error {
	select {
	case g.entered <- struct{}{}:
	default:
	}
	<-g.gate
	return g.mockPush.AllocatePushTransport(ctx, id, fabric, o, c)
}

// TestPushAllocateRunsOneAtATime holds the per-fabric bound across the
// port call: with MaxPerFabric 1, a second AllocatePushTransport of the
// fabric that arrives while the first waits on the port is answered
// RESOURCE_EXHAUSTED after it. Before, both passed the count before
// either was stored, and the fabric held two transports (review II,
// R2-P6-conc-1609).
func TestPushAllocateRunsOneAtATime(t *testing.T) {
	t.Parallel()
	g := gatedPush{mockPush: newMockPush(), entered: make(chan struct{}, 1), gate: make(chan struct{})}
	p, _ := newPush(t, func(c *camera.PushTransportConfig) { c.Port, c.MaxPerFabric = g, 1 })
	req := padef.AllocatePushTransportRequest{TransportOptions: motionOptions()}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := pushInvoke(p, padef.CmdAllocatePushTransport, req); first <- err }()
	<-g.entered
	go func() { _, err := pushInvoke(p, padef.CmdAllocatePushTransport, req); second <- err }()
	settle(g.entered)
	close(g.gate)
	wantCode(t, "first", statusOf(<-first), im.StatusSuccess)
	wantCode(t, "second", statusOf(<-second), im.StatusResourceExhausted)
	if n := len(p.Connections()); n != 1 {
		t.Fatalf("%d connections, want 1", n)
	}
}
