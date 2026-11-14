// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
)

// rearmingServer is an EVSE-shaped server: MatterQuiesce stops its timer,
// and the SetEndpoint of a reassembly that puts it back re-arms it
// (cluster/energy EvseServer.SetEndpoint, cluster/thermo
// ThermostatServer.SetEndpoint). afterSetEndpoint, when set, runs after
// each SetEndpoint did its work.
type rearmingServer struct {
	mu               sync.Mutex
	quiesced         bool
	armed            bool
	afterSetEndpoint func()
}

func (*rearmingServer) MatterClusterID() uint32                                { return 0x0099 }
func (*rearmingServer) MatterRead(uint32) (any, bool)                          { return uint8(0), true }
func (*rearmingServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*rearmingServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*rearmingServer) MatterReportable() []uint32                             { return nil }
func (*rearmingServer) SetMatterEventEmitter(contract.EventEmitter)            {}
func (s *rearmingServer) setAfterSetEndpoint(fn func()) {
	s.mu.Lock()
	s.afterSetEndpoint = fn
	s.mu.Unlock()
}

func (s *rearmingServer) state() (quiesced, armed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quiesced, s.armed
}

func (s *rearmingServer) MatterQuiesce() {
	s.mu.Lock()
	s.quiesced, s.armed = true, false
	s.mu.Unlock()
}

func (s *rearmingServer) SetEndpoint(uint16) {
	s.mu.Lock()
	if s.quiesced {
		s.quiesced, s.armed = false, true
	}
	after := s.afterSetEndpoint
	s.mu.Unlock()
	if after != nil {
		after()
	}
}

// switchedSnapshotter returns the root, the aggregator and, while with is
// set, the endpoint carrying srv. A call numbered hold (1-based) waits for
// release after closing entered.
type switchedSnapshotter struct {
	ep               *endpointpkg.Endpoint
	mu               sync.Mutex
	with             bool
	calls, hold      int
	entered, release chan struct{}
}

func newSwitchedSnapshotter(srv contract.ClusterServer) *switchedSnapshotter {
	return &switchedSnapshotter{
		ep: &endpointpkg.Endpoint{
			ID: 7, DeviceType: 0x0101, SourceKey: endpointpkg.StringKey("evse"), Scope: "s", DeviceAddress: "e",
			Source: &serverSource{servers: []contract.ClusterServer{srv}},
		},
		with: true, entered: make(chan struct{}), release: make(chan struct{}),
	}
}

func (s *switchedSnapshotter) set(with bool, hold int) {
	s.mu.Lock()
	s.with, s.hold, s.calls = with, hold, 0
	s.mu.Unlock()
}

func (s *switchedSnapshotter) snapshot(context.Context) (*endpointpkg.Topology, error) {
	s.mu.Lock()
	s.calls++
	held, with := s.calls == s.hold, s.with
	s.mu.Unlock()
	if held {
		close(s.entered)
		<-s.release
	}
	eps := []*endpointpkg.Endpoint{{ID: 0, DeviceType: 0x0016}, {ID: 1, Aggregator: true, DeviceType: 0x000E}}
	if with {
		eps = append(eps, s.ep)
	}
	return &endpointpkg.Topology{Endpoints: eps}, nil
}

// await waits for ch or fails the test when ctx ends first.
func await(ctx context.Context, t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatalf("%s: %v", what, ctx.Err())
	}
}

// TestReassembleOverlappingStopLeavesNoTimerArmed (review II,
// R2-P1-rw-04): the host removes an EVSE-shaped server (the reassembly
// quiesces it) and adds it back; that Reassemble passed its started check
// before a Stop ran. Its SetEndpoint re-arms the server — then it finds
// the run over and returns ErrNotStarted. The server must not be left
// running on the stopped bridge.
func TestReassembleOverlappingStopLeavesNoTimerArmed(t *testing.T) {
	t.Parallel()
	srv := &rearmingServer{armed: true}
	snap := newSwitchedSnapshotter(srv)
	b := newStartedBridgeWithSnapshotter(t, snap.snapshot)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	snap.set(false, 0)
	if err := b.Reassemble(ctx); err != nil {
		t.Fatalf("the Reassemble removing the server: %v", err)
	}
	if q, _ := srv.state(); !q {
		t.Fatal("the removed server was not quiesced")
	}

	snap.set(true, 1)
	reassembled := make(chan error, 1)
	go func() { reassembled <- b.Reassemble(ctx) }()
	await(ctx, t, snap.entered, "the Reassemble never reached its snapshot")
	if err := b.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	close(snap.release)
	if err := <-reassembled; !errors.Is(err, ErrNotStarted) {
		t.Fatalf("the Reassemble overlapping Stop returned %v, want ErrNotStarted", err)
	}
	if q, armed := srv.state(); armed || !q {
		t.Fatalf("the server after a Reassemble that found its run over: quiesced %v, armed %v; want quiesced on the stopped bridge", q, armed)
	}
}

// TestConcurrentReassemblesLeaveTheLiveServerRunning (review II,
// R2-P1-rw-04, the variant): two Reassembles run at once — R2 keeps the
// server, R1 removes it. R2's SetEndpoint runs first (the server is live,
// nothing to re-arm), then R1 swaps its topology in and quiesces the
// server, then R2 swaps in the topology that carries it. The server the
// live topology serves must run.
func TestConcurrentReassemblesLeaveTheLiveServerRunning(t *testing.T) {
	t.Parallel()
	srv := &rearmingServer{armed: true}
	snap := newSwitchedSnapshotter(srv)
	b := newStartedBridgeWithSnapshotter(t, snap.snapshot)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	inR2, releaseR2 := make(chan struct{}), make(chan struct{})
	var once sync.Once
	srv.setAfterSetEndpoint(func() {
		once.Do(func() {
			close(inR2)
			<-releaseR2
		})
	})
	r2 := make(chan error, 1)
	go func() { r2 <- b.Reassemble(ctx) }() // keeps the server
	await(ctx, t, inR2, "R2 never reached the server's SetEndpoint")
	snap.set(false, 0)
	if err := b.Reassemble(ctx); err != nil { // R1 removes it
		t.Fatalf("R1: %v", err)
	}
	close(releaseR2)
	if err := <-r2; err != nil {
		t.Fatalf("R2: %v", err)
	}
	if !topologyCarries(b.Topology(), srv) {
		t.Fatal("R2's swap did not come last: the scenario needs the live topology to carry the server")
	}
	if q, armed := srv.state(); q || !armed {
		t.Fatalf("the server the live topology serves: quiesced %v, armed %v; want running", q, armed)
	}
}

// topologyCarries reports whether srv serves on one of topo's endpoints.
func topologyCarries(topo *endpointpkg.Topology, srv contract.ClusterQuiescer) bool {
	for _, q := range quiescers(topo) {
		if q == srv {
			return true
		}
	}
	return false
}
