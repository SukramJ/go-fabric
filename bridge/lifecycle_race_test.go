// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// heldSnapshotter assembles one contact endpoint whose host source counts
// the bridge's listeners; the call numbered hold (1-based) waits until
// release closes, after closing entered.
type heldSnapshotter struct {
	src              *countingContact
	asm              *endpoint.Assembler
	hold             int
	entered, release chan struct{}

	mu    sync.Mutex
	calls int
}

func newHeldSnapshotter(t *testing.T, hold int) *heldSnapshotter {
	t.Helper()
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &heldSnapshotter{src: &countingContact{}, asm: asm, hold: hold, entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *heldSnapshotter) snapshot(ctx context.Context) (*endpoint.Topology, error) {
	h.mu.Lock()
	h.calls++
	held := h.calls == h.hold
	h.mu.Unlock()
	if held {
		close(h.entered)
		<-h.release
	}
	return h.asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
		StableKey: endpoint.StringKey("contact"), DeviceAddress: "C", DeviceType: 0x0015,
		FriendlyName: "Contact", ChannelAddress: "C:1", Measurement: h.src,
	}}}})
}

func (b *Bridge) isStarted() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.started
}

// TestStopDuringStartWaitsForIt (review II, R2-P1-req2-05): a Stop that
// arrives while a Start is still running waits for it and then stops the
// run — it neither returns at once and lets the bridge come up after it,
// nor tears down a run whose ACK pump and radio Start goes on to start.
func TestStopDuringStartWaitsForIt(t *testing.T) {
	t.Parallel()
	snap := newHeldSnapshotter(t, 1)
	b, err := New(snap.snapshot, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.AttachAckTracker(mrp.NewAckTracker(mrp.DefaultStandaloneAckDelay))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	started := make(chan error, 1)
	go func() { started <- b.Start(ctx) }()
	<-snap.entered

	stopped := make(chan error, 1)
	go func() { stopped <- b.Stop(ctx) }()
	early, earlyCancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer earlyCancel()
	select {
	case err := <-stopped:
		close(snap.release)
		<-started
		t.Fatalf("Stop returned (%v) while a Start was still running; the bridge came up after it (started=%v)", err, b.isStarted())
	case <-early.Done():
	}
	close(snap.release)
	if err := <-started; err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := <-stopped; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	b.mu.RLock()
	running, pump := b.started, b.pumpCancel
	b.mu.RUnlock()
	if running || pump != nil {
		t.Fatalf("after Start and the Stop that followed it: started=%v, ACK pump left running=%v", running, pump != nil)
	}
	if got := snap.src.live.Load(); got != 0 {
		t.Errorf("%d host listeners registered after Stop returned, want 0", got)
	}
}

// TestFailedStartLeavesNoHostListeners (review II, R2-P1-req2-06): a Start
// that fails after its initial assembly wired the host's listeners
// detaches them again — Stop on the bridge that never started does not.
func TestFailedStartLeavesNoHostListeners(t *testing.T) {
	t.Parallel()
	busy, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = busy.Close() })
	snap := newHeldSnapshotter(t, 0)
	b, err := New(snap.snapshot, mdns.NewNoop(), Config{Listen: busy.LocalAddr().String()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	if err := b.Start(t.Context()); err == nil {
		t.Fatal("Start on a busy port succeeded; the test needs it to fail after the assembly")
	}
	if got := snap.src.live.Load(); got != 0 {
		t.Fatalf("%d host listeners registered after a failed Start, want 0", got)
	}
}

// TestReassembleOverlappingStopLeavesNoHostListeners (review II,
// R2-P1-req2-06): a Reassemble that passed its started check before a Stop
// ran to its end does not wire the host's listeners on the stopped bridge
// afterwards; it returns ErrNotStarted.
func TestReassembleOverlappingStopLeavesNoHostListeners(t *testing.T) {
	t.Parallel()
	snap := newHeldSnapshotter(t, 2)
	b := newStartedBridgeWithSnapshotter(t, snap.snapshot)
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	if got := snap.src.live.Load(); got != 1 {
		t.Fatalf("%d listeners on the started bridge, want 1", got)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	reassembled := make(chan error, 1)
	go func() { reassembled <- b.Reassemble(ctx) }()
	<-snap.entered
	if err := b.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	close(snap.release)
	if err := <-reassembled; !errors.Is(err, ErrNotStarted) {
		t.Errorf("Reassemble overlapping Stop returned %v, want ErrNotStarted", err)
	}
	if got := snap.src.live.Load(); got != 0 {
		t.Fatalf("%d host listeners registered on the stopped bridge, want 0", got)
	}
}
