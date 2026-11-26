// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
)

// gatedStoreW2B5 is an internalFakeStore safe for concurrent use whose
// lookup of the key gated waits until gate is closed, so a test can hold
// an assembly between two of its specs.
type gatedStoreW2B5 struct {
	mu    sync.Mutex
	inner *internalFakeStore
	gated SourceKey
	gate  chan struct{}
}

func (s *gatedStoreW2B5) GetEndpoint(ctx context.Context, key SourceKey) (Record, error) {
	if key == s.gated {
		<-s.gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.GetEndpoint(ctx, key)
}

func (s *gatedStoreW2B5) UpsertEndpointAssigning(ctx context.Context, rec Record) (uint16, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.UpsertEndpointAssigning(ctx, rec)
}

func (s *gatedStoreW2B5) ListEndpoints(ctx context.Context, scope string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.ListEndpoints(ctx, scope)
}

func (s *gatedStoreW2B5) RemoveEndpoint(ctx context.Context, key SourceKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.RemoveEndpoint(ctx, key)
}

func (s *gatedStoreW2B5) EraseEndpoints(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.EraseEndpoints(ctx)
}

// TestConcurrentAssembliesShareTheFirstStateW2B5 (issue #618, decision
// 687): two concurrent Assemble runs of a key new to both end with one
// state — the endpoint of every topology they return holds the state the
// registry keeps for the key. Each run builds key "a" and then waits in the
// store on key "b"; before Assemble serialised itself both runs had built
// their own first state of "a" by then, and the install that lost left its
// topology with a state the registry does not know.
func TestConcurrentAssembliesShareTheFirstStateW2B5(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := &gatedStoreW2B5{inner: newInternalFakeStore(), gated: StringKey("b"), gate: make(chan struct{})}
		a, err := New(st, Config{VendorID: 0xFFF1, ProductID: 0x8001}, nil)
		if err != nil {
			t.Fatal(err)
		}
		snap := []Snapshot{{Scope: "s", Endpoints: []Spec{flowSpec(StringKey("a"), "A"), flowSpec(StringKey("b"), "B")}}}
		var (
			wg    sync.WaitGroup
			topos [2]*Topology
			errs  [2]error
		)
		for i := range topos {
			wg.Go(func() { topos[i], errs[i] = a.Assemble(context.Background(), snap) })
		}
		synctest.Wait()
		close(st.gate)
		wg.Wait()

		a.states.mu.Lock()
		want := a.states.states[StringKey("a")]
		a.states.mu.Unlock()
		if want == nil {
			t.Fatal("the registry holds no state for key a")
		}
		for i, topo := range topos {
			if errs[i] != nil {
				t.Fatalf("assembly %d: %v", i, errs[i])
			}
			for _, ep := range topo.Endpoints {
				if ep.SourceKey != nil && ep.SourceKey.String() == "a" && ep.state != want {
					t.Errorf("assembly %d: endpoint a holds a state the registry does not know", i)
				}
			}
		}
	})
}

// TestAssembleWaitEndsWithContextW2B5 (issue #618): an Assemble that waits
// for one in progress returns once its context ends, with the context's
// error, and builds nothing.
func TestAssembleWaitEndsWithContextW2B5(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := &gatedStoreW2B5{inner: newInternalFakeStore(), gated: StringKey("b"), gate: make(chan struct{})}
		a, err := New(st, Config{VendorID: 0xFFF1, ProductID: 0x8001}, nil)
		if err != nil {
			t.Fatal(err)
		}
		held := []Snapshot{{Scope: "s", Endpoints: []Spec{flowSpec(StringKey("b"), "B")}}}
		var wg sync.WaitGroup
		wg.Go(func() { _, _ = a.Assemble(context.Background(), held) })
		synctest.Wait()

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := a.Assemble(ctx, []Snapshot{{Scope: "s", Endpoints: []Spec{flowSpec(StringKey("c"), "C")}}})
			done <- err
		}()
		synctest.Wait()
		select {
		case err := <-done:
			close(st.gate)
			wg.Wait()
			t.Fatalf("Assemble returned beside one in progress: %v", err)
		default:
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Assemble err = %v, want context.Canceled", err)
		}
		st.mu.Lock()
		_, built := st.inner.rows[StringKey("c")]
		st.mu.Unlock()
		if built {
			t.Error("an Assemble whose wait ended stored its endpoint")
		}
		close(st.gate)
		wg.Wait()
	})
}
