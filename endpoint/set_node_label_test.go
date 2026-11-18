// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
)

// labelStore is a [Persistence] that records the label it stored last
// and refuses while refuse is set.
type labelStore struct {
	mu     sync.Mutex
	refuse error
	last   string
	saves  int
}

func (s *labelStore) SaveNodeLabel(_ context.Context, _ SourceKey, label string) error {
	s.mu.Lock()
	refuse := s.refuse
	s.mu.Unlock()
	if refuse != nil {
		return refuse
	}
	// Widen the window between a write's store and its set, so an
	// unserialised pair of writes interleaves.
	runtime.Gosched()
	s.mu.Lock()
	s.last = label
	s.saves++
	s.mu.Unlock()
	return nil
}

func (*labelStore) LoadScenes(context.Context, SourceKey) ([]byte, error) { return nil, nil }

func (*labelStore) SaveScenes(context.Context, SourceKey, []byte) error { return nil }

func (s *labelStore) stored() (last string, saves int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.saves
}

// TestSetNodeLabelReturnsTheStoresRefusal (R2-P5-apidev-16, decision
// 226): the host's rename is stored before it is served; a refused store
// serves nothing and reaches the caller.
func TestSetNodeLabelReturnsTheStoresRefusal(t *testing.T) {
	t.Parallel()
	refused := errors.New("store refused")
	p := &labelStore{refuse: refused}
	e := &Endpoint{FriendlyName: "spec", SourceKey: StringKey("k"), persistence: p}
	if err := e.SetNodeLabel("renamed"); !errors.Is(err, refused) {
		t.Fatalf("SetNodeLabel = %v, want the store's refusal", err)
	}
	if got := e.NodeLabel(); got != "spec" {
		t.Errorf("a refused rename is served: %q", got)
	}
	p.mu.Lock()
	p.refuse = nil
	p.mu.Unlock()
	if err := e.SetNodeLabel("renamed"); err != nil {
		t.Fatalf("SetNodeLabel: %v", err)
	}
	if got, saves := p.stored(); got != "renamed" || saves != 1 || e.NodeLabel() != "renamed" {
		t.Errorf("stored %q (%d saves), served %q; want one save of %q, served", got, saves, e.NodeLabel(), "renamed")
	}
}

// TestNodeLabelServedIsStoredLast (R2-P5-apidev-16, decision 226): host
// renames and controller writes (the server's saver hook) running at once
// end with the label served equal to the label stored last.
func TestNodeLabelServedIsStoredLast(t *testing.T) {
	t.Parallel()
	for round := range 50 {
		p := &labelStore{}
		e := &Endpoint{FriendlyName: "spec", SourceKey: StringKey("k"), persistence: p}
		save, hook := e.nodeLabelSaver(), e.nodeLabelWriter()
		if save == nil {
			t.Fatal("no saver for an endpoint with a persistence and a key")
		}
		var wg sync.WaitGroup
		for i := range 8 {
			label := fmt.Sprintf("label-%d", i)
			wg.Go(func() {
				if i%2 == 0 {
					if err := e.SetNodeLabel(label); err != nil {
						t.Errorf("SetNodeLabel: %v", err)
					}
					return
				}
				// The order the BridgedDeviceBasicInformation server
				// runs its hooks in: store, then hold.
				if err := save(context.Background(), label); err != nil {
					t.Errorf("save: %v", err)
					return
				}
				if hook != nil {
					hook(label)
				}
			})
		}
		wg.Wait()
		if stored, _ := p.stored(); e.NodeLabel() != stored {
			t.Fatalf("round %d: served %q, stored %q", round, e.NodeLabel(), stored)
		}
	}
}
