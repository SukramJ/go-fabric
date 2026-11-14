// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
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

// TestStopWithDoneContextTearsDown (review II, R2-P1-dr-req-01): a Stop
// whose context is already done when no Start or Stop is running still
// tears the run down — only its waits are bounded by ctx — on every call,
// not on the half of them a select between the free lifecycle slot and
// ctx.Done() picks the slot. A bridge is single-run (R2-P2-stop-05), so
// every run is a new bridge; each binds the same port, so each Start
// succeeding shows the Stop before it released the socket.
func TestStopWithDoneContextTearsDown(t *testing.T) {
	t.Parallel()
	probe, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.LocalAddr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	snap := newHeldSnapshotter(t, 0)
	done, cancel := context.WithCancel(t.Context())
	cancel()
	const runs = 200
	for i := range runs {
		b, err := New(snap.snapshot, mdns.NewNoop(), Config{Listen: addr}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Start(t.Context()); err != nil {
			t.Fatalf("run %d: Start on %s: %v", i, addr, err)
		}
		if err := b.Stop(done); err != nil {
			t.Fatalf("run %d: Stop with a done context: %v", i, err)
		}
		if b.isStarted() || b.LocalAddr() != "" || b.startClaim.Load() {
			t.Fatalf("run %d: after Stop with a done context: started=%v addr=%q claim=%v", i, b.isStarted(), b.LocalAddr(), b.startClaim.Load())
		}
	}
	free, err := net.ListenPacket("udp", addr)
	if err != nil {
		t.Fatalf("the port after the last Stop: %v", err)
	}
	_ = free.Close()
}

// gatedContact is a contact source that counts its live value listeners;
// once armed, its next registration closes entered and waits for release
// — a host notifier that blocks while the bridge registers with it.
type gatedContact struct {
	countingContact
	armed            atomic.Bool
	entered, release chan struct{}
}

func (s *gatedContact) OnMatterValueChanged(fn func()) func() {
	if s.armed.CompareAndSwap(true, false) {
		close(s.entered)
		<-s.release
	}
	return s.countingContact.OnMatterValueChanged(fn)
}

// TestReassembleStraddlingARestartKeepsTheNewRunsListeners (review II,
// R2-P1-dr-req-05): a Reassemble that blocks in a host notifier's
// registration while the bridge is stopped, and resumes to find its run
// over, leaves no listener behind — it removes what it wired itself. A
// bridge is single-run since R2-P2-stop-05: the Start attempted in
// between is refused with ErrStopped, so no new run's listeners exist
// for the resumed Reassemble to keep. Before R2-P1-dr-req-05 it detached
// whatever the bridge held, and a new run served without its value
// listeners until the next Reassemble (the state R2-P0-rv-A06 was fixed
// for).
func TestReassembleStraddlingARestartKeepsTheNewRunsListeners(t *testing.T) {
	t.Parallel()
	src := &gatedContact{entered: make(chan struct{}), release: make(chan struct{})}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("contact"), DeviceAddress: "C", DeviceType: 0x0015,
			FriendlyName: "Contact", ChannelAddress: "C:1", Measurement: src,
		}}}})
	}
	b := newStartedBridgeWithSnapshotter(t, snap)
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	if got := src.live.Load(); got != 1 {
		t.Fatalf("%d listeners on the started bridge, want 1", got)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	src.armed.Store(true)
	reassembled := make(chan error, 1)
	go func() { reassembled <- b.Reassemble(ctx) }()
	select {
	case <-src.entered:
	case <-ctx.Done():
		t.Fatal("the Reassemble never registered with the host's notifier")
	}
	if err := b.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := b.Start(ctx); !errors.Is(err, ErrStopped) {
		t.Fatalf("the second Start = %v, want ErrStopped", err)
	}
	close(src.release)
	<-reassembled
	if got := src.live.Load(); got != 0 {
		t.Fatalf("%d listeners once the Reassemble of the stopped run returned, want 0", got)
	}
}

// TestAttachSubscriptionManagerAfterStopWiresNoHostListeners (review II,
// R2-P2-sess-09, N-bridge2-4): a subscription manager attached to a
// stopped bridge is stored and nothing else — the host's notifiers, whose
// listeners Stop detached, are not wired to the stopped bridge again.
// Attached to a running bridge, it is wired.
func TestAttachSubscriptionManagerAfterStopWiresNoHostListeners(t *testing.T) {
	t.Parallel()
	snap := newHeldSnapshotter(t, 0)
	b, err := New(snap.snapshot, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	if got := snap.src.live.Load(); got == 0 {
		t.Fatal("a manager attached to the running bridge wired no host listener")
	}
	if err := b.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := snap.src.live.Load(); got != 0 {
		t.Fatalf("%d host listeners registered after Stop, want 0", got)
	}

	m := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(m)
	if got := snap.src.live.Load(); got != 0 {
		t.Fatalf("%d host listeners registered by an attach after Stop, want 0", got)
	}
	if b.subscriptionManagerLocked() != m {
		t.Fatal("the attached manager was not stored")
	}
}

// heldLoadStore is a SubscriptionStore whose load — the step of Start
// between its initial assembly and the run's start — waits for release.
type heldLoadStore struct {
	*memSubscriptionStore
	entered, release chan struct{}
}

func (s *heldLoadStore) LoadServerSubscriptions(ctx context.Context) ([][]byte, error) {
	close(s.entered)
	<-s.release
	return s.memSubscriptionStore.LoadServerSubscriptions(ctx)
}

// TestAttachSubscriptionManagerDuringStartIsWired (review II,
// R2-P2-dr-req-05): a subscription manager attached while Start runs —
// after its initial assembly wired the host's listeners without a
// manager, before the run counts as started — is wired once Start
// completes, so the host's value changes mark the subscriptions dirty
// without waiting for the next Reassemble.
func TestAttachSubscriptionManagerDuringStartIsWired(t *testing.T) {
	t.Parallel()
	snap := newHeldSnapshotter(t, 0)
	b, err := New(snap.snapshot, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := &heldLoadStore{memSubscriptionStore: newMemSubscriptionStore(), entered: make(chan struct{}), release: make(chan struct{})}
	b.AttachSubscriptionStore(st)
	started := make(chan error, 1)
	go func() { started <- b.Start(t.Context()) }()
	<-st.entered
	if got := snap.src.live.Load(); got != 0 {
		t.Fatalf("%d host listeners after an assembly without a manager, want 0", got)
	}
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	close(st.release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })
	if got := snap.src.live.Load(); got == 0 {
		t.Fatal("a manager attached while Start ran wired no host listener")
	}
}
