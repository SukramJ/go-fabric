// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
)

// nodeBindingSync is a member node whose Binding lists both writes reach:
// a push's whole-endpoint write (SyncEndpointBindings) and a refresh's
// whole-node write (SyncBindings, which leaves an endpoint it names no
// entry for empty). The first fetch a test arms waits after it read the
// list until released.
type nodeBindingSync struct {
	trackingSync
	mu2      sync.Mutex
	device   map[uint16][]EndpointBindingEntry
	writeErr error
	hold     chan struct{}
	held     chan struct{}
}

func (s *nodeBindingSync) FetchEndpointBindingList(_ context.Context, _ uint64, ep uint16) ([]EndpointBindingEntry, error) {
	s.mu2.Lock()
	list, hold, held := slices.Clone(s.device[ep]), s.hold, s.held
	s.hold = nil
	s.mu2.Unlock()
	if hold != nil {
		close(held)
		<-hold
	}
	return list, nil
}

func (s *nodeBindingSync) SyncEndpointBindings(_ context.Context, _ uint64, ep uint16, entries []EndpointBindingEntry) error {
	s.mu2.Lock()
	defer s.mu2.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	s.device[ep] = slices.Clone(entries)
	return nil
}

func (s *nodeBindingSync) SyncBindings(_ context.Context, _ uint64, entries []EndpointBindingEntry) error {
	s.mu2.Lock()
	defer s.mu2.Unlock()
	for ep := range s.device {
		s.device[ep] = nil
	}
	for _, e := range entries {
		s.device[e.EndpointId] = append(s.device[e.EndpointId], e)
	}
	return nil
}

// TestRefreshAndBindingPushDoNotLoseAWrite: a binding push to a node and
// that node's refresh run at once. The push's fetch, merge and write and
// the refresh's whole-list write are serialised per node, so a binding the
// refresh erases does not come back with the push's write of the list it
// read before the refresh wrote (review II, R2-P1-clu-05).
func TestRefreshAndBindingPushDoNotLoseAWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		s := &nodeBindingSync{trackingSync: *newTrackingSync(), device: map[uint16][]EndpointBindingEntry{}}
		s.endpointsToFetch = []EndpointEntry{{EndpointId: 1}}
		d := newStore(t, s)
		mustOK(t, d.AddPendingNode(ctx, 123, "controller-a"))
		d.addEndpointEntry(ctx, 1, 123, "endpoint-a")
		b := BindingTarget{Group: ptr[uint16](10)}
		c := BindingTarget{Node: ptr[uint64](0x1111), Endpoint: ptr[uint16](2)}
		mustOK(t, d.AddBindingToEndpointForNode(ctx, 123, 1, b))
		// B's removal cannot reach the node: it stays DeletePending in the
		// datastore and on the node, for the refresh to erase.
		var listID uint16
		for _, e := range d.EndpointBindingList() {
			listID = e.ListId
		}
		s.mu2.Lock()
		s.writeErr = errors.New("node unreachable")
		s.mu2.Unlock()
		if err := d.RemoveBindingFromEndpointForNode(ctx, listID, 123, 1); err == nil {
			t.Fatal("the removal's push succeeded against an unreachable node")
		}
		s.mu2.Lock()
		s.writeErr = nil
		s.hold, s.held = make(chan struct{}), make(chan struct{})
		hold, held := s.hold, s.held
		s.mu2.Unlock()

		pushed := make(chan error, 1)
		go func() { pushed <- d.AddBindingToEndpointForNode(ctx, 123, 1, c) }()
		<-held // the push read the node's list, B still on it
		refreshed := make(chan error, 1)
		go func() { refreshed <- d.RefreshNode(ctx, 123) }()
		synctest.Wait()
		close(hold)
		mustOK(t, <-pushed)
		mustOK(t, <-refreshed)

		s.mu2.Lock()
		onNode := slices.Clone(s.device[1])
		s.mu2.Unlock()
		if slices.ContainsFunc(onNode, func(e EndpointBindingEntry) bool { return bindingMatches(e.Binding, b) }) {
			t.Errorf("the node's list %+v holds the binding the refresh erased", onNode)
		}
		if !slices.ContainsFunc(onNode, func(e EndpointBindingEntry) bool { return bindingMatches(e.Binding, c) }) {
			t.Errorf("the node's list %+v lacks the binding the push added", onNode)
		}
		if slices.ContainsFunc(d.EndpointBindingList(), func(e EndpointBindingEntry) bool { return bindingMatches(e.Binding, b) }) {
			t.Errorf("EndpointBindingList %+v still holds the erased binding", d.EndpointBindingList())
		}
	})
}
