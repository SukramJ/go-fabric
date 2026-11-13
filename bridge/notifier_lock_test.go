// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// A host notifier is called — to register, to unregister — outside the
// bridge's mutex (review II, W2-conc-07), and Stop unregisters what the
// last wiring registered (W2-conc-10). The callback the bridge registers
// re-enters the bridge: a BooleanState change emits StateChange through
// EmitEvent, which reads the subscription manager under b.mu. Before,
// the registration and the stale unsubscribers ran while Reassemble and
// AttachSubscriptionManager held b.mu for writing, so a notifier within
// its contract — one whose unsubscribe waits for a callback in flight, or
// one that replays its value while registering — stopped the bridge for
// good.

// waitingContact is a contact source whose unsubscribe waits for the
// callback in flight, and which can replay its value while a callback
// registers.
type waitingContact struct {
	mu     sync.Mutex
	cb     func()
	value  bool
	replay bool // fire the callback inside OnMatterValueChanged

	inflight     sync.WaitGroup
	unsubEntered chan struct{} // closed when the first unsubscribe runs
	unsubOnce    sync.Once
	unsubs       atomic.Int32
}

func newWaitingContact() *waitingContact {
	return &waitingContact{unsubEntered: make(chan struct{})}
}

func (*waitingContact) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementContact
}

func (s *waitingContact) MatterBoolValue() (value, observed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, true
}

func (s *waitingContact) OnMatterValueChanged(cb func()) func() {
	s.mu.Lock()
	s.cb = cb
	replay := s.replay
	if replay {
		s.value = !s.value // the replay carries a change, so the bridge emits
	}
	s.mu.Unlock()
	if replay {
		cb()
	}
	return func() {
		s.unsubs.Add(1)
		s.unsubOnce.Do(func() { close(s.unsubEntered) })
		s.inflight.Wait()
	}
}

// fireOnceUnsubscribing runs one callback that reaches the bridge only
// after the unsubscribe has begun to wait for it.
func (s *waitingContact) fireOnceUnsubscribing() {
	s.mu.Lock()
	cb := s.cb
	s.mu.Unlock()
	s.inflight.Add(1)
	go func() {
		defer s.inflight.Done()
		<-s.unsubEntered
		s.mu.Lock()
		s.value = !s.value
		s.mu.Unlock()
		cb()
	}()
}

func contactBridge(t *testing.T, src *waitingContact) *Bridge {
	t.Helper()
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return newStartedBridgeWithSnapshotter(t, func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("contact"), DeviceAddress: "C", DeviceType: 0x0015,
			FriendlyName: "Contact", ChannelAddress: "C:1", Measurement: src,
		}}}})
	})
}

// returnsWithin fails the test when f has not returned after five
// seconds: the bridge is then stuck on its mutex.
func returnsWithin(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return within 5 s: a host notifier was called under the bridge's mutex", what)
	}
}

func TestReassembleUnsubscribesOutsideTheBridgeMutex(t *testing.T) {
	t.Parallel()
	src := newWaitingContact()
	b := contactBridge(t, src)
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))

	src.fireOnceUnsubscribing()
	returnsWithin(t, "Reassemble with a callback in flight", func() {
		if err := b.Reassemble(context.Background()); err != nil {
			t.Errorf("Reassemble: %v", err)
		}
	})
	if n := src.unsubs.Load(); n != 1 {
		t.Errorf("unsubscribe calls after the reassemble = %d, want 1 (the superseded listener)", n)
	}
}

func TestNotifierMayCallBackWhileItRegisters(t *testing.T) {
	t.Parallel()
	src := newWaitingContact()
	b := contactBridge(t, src)
	src.mu.Lock()
	src.replay = true
	src.mu.Unlock()
	returnsWithin(t, "AttachSubscriptionManager with a notifier that replays on registration", func() {
		b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	})
}

func TestStopUnsubscribesTheHostNotifiers(t *testing.T) {
	t.Parallel()
	src := newWaitingContact()
	b := contactBridge(t, src)
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	if n := src.unsubs.Load(); n != 0 {
		t.Fatalf("unsubscribe calls before Stop = %d, want 0", n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if n := src.unsubs.Load(); n != 1 {
		t.Errorf("unsubscribe calls after Stop = %d, want 1 — the host's notifier still calls into a stopped bridge", n)
	}
}
