// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/im"
)

// endpointSync is a DatastoreSync that is an EndpointBindingSync: it
// holds the member node's Binding list per endpoint, as jf-admin-app's
// delegate reads it (examples/jf-admin-app/linux/JFADatastoreSync.cpp:1717-1719
// at 6170af84), and records each whole-list write.
type endpointSync struct {
	trackingSync
	mu2       sync.Mutex
	device    map[uint16][]EndpointBindingEntry
	fetchErr  error
	writes    []endpointWrite
	writeErr  error
	fetchCall int
}

type endpointWrite struct {
	nodeID   uint64
	endpoint uint16
	entries  []EndpointBindingEntry
}

func newEndpointSync() *endpointSync {
	return &endpointSync{trackingSync: *newTrackingSync(), device: map[uint16][]EndpointBindingEntry{}}
}

func (s *endpointSync) FetchEndpointBindingList(_ context.Context, _ uint64, ep uint16) ([]EndpointBindingEntry, error) {
	s.mu2.Lock()
	defer s.mu2.Unlock()
	s.fetchCall++
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	return slices.Clone(s.device[ep]), nil
}

func (s *endpointSync) SyncEndpointBindings(_ context.Context, nodeID uint64, ep uint16, entries []EndpointBindingEntry) error {
	s.mu2.Lock()
	defer s.mu2.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	s.writes = append(s.writes, endpointWrite{nodeID: nodeID, endpoint: ep, entries: slices.Clone(entries)})
	s.device[ep] = slices.Clone(entries)
	return nil
}

// nodeWithEndpointSync is nodeWithEndpoint over an endpointSync.
func nodeWithEndpointSync(t *testing.T, s *endpointSync) *Datastore {
	t.Helper()
	d := newStore(t, s)
	mustOK(t, d.AddPendingNode(t.Context(), 123, "controller-a"))
	d.addEndpointEntry(t.Context(), 1, 123, "endpoint-a")
	return d
}

func (s *endpointSync) lastWrite(t *testing.T) endpointWrite {
	t.Helper()
	s.mu2.Lock()
	defer s.mu2.Unlock()
	if len(s.writes) == 0 {
		t.Fatal("no SyncEndpointBindings call")
	}
	return s.writes[len(s.writes)-1]
}

// TestEndpointBindingSyncAppendsToFetchedList: a port that is an
// EndpointBindingSync takes the add of a binding as the endpoint's
// current list plus the entry (JFADatastoreSync.cpp:1717-1747 at
// 6170af84); SyncBinding is not called; the entry commits.
func TestEndpointBindingSyncAppendsToFetchedList(t *testing.T) {
	s := newEndpointSync()
	existing := BindingTarget{Node: ptr[uint64](0x2222), Endpoint: ptr[uint16](3)}
	s.device[1] = []EndpointBindingEntry{{EndpointId: 1, Binding: existing}}
	d := nodeWithEndpointSync(t, s)
	added := BindingTarget{Node: ptr[uint64](0x1111), Endpoint: ptr[uint16](2)}
	mustOK(t, d.AddBindingToEndpointForNode(t.Context(), 123, 1, added))
	w := s.lastWrite(t)
	if w.nodeID != 123 || w.endpoint != 1 || len(w.entries) != 2 || !bindingMatches(w.entries[0].Binding, existing) || !bindingMatches(w.entries[1].Binding, added) {
		t.Fatalf("SyncEndpointBindings(%d, %d, %+v)", w.nodeID, w.endpoint, w.entries)
	}
	if s.lastBinding != nil {
		t.Fatalf("SyncBinding called with %+v", s.lastBinding)
	}
	b := d.EndpointBindingList()
	if len(b) != 1 || b[0].StatusEntry.State != stateCommitted {
		t.Fatalf("EndpointBindingList = %+v", b)
	}
	if e := w.entries[1]; e.StatusEntry.State != statePending || e.ListId != b[0].ListId || e.NodeId != 123 {
		t.Fatalf("the datastore's entry travels as it is, Pending: %+v", e)
	}
}

// TestEndpointBindingSyncRemovesByTarget: the remove of a binding writes
// the endpoint's list without the entries whose target matches — the
// node's entries carry no list id, so the match is by node, group,
// endpoint and cluster (JFADatastoreSync.cpp:1731-1741 at 6170af84); the
// last binding's removal writes the empty list (:663 at 6170af84).
func TestEndpointBindingSyncRemovesByTarget(t *testing.T) {
	s := newEndpointSync()
	d := nodeWithEndpointSync(t, s)
	keep := BindingTarget{Group: ptr[uint16](10)}
	gone := BindingTarget{Node: ptr[uint64](0x3333), Endpoint: ptr[uint16](1), Cluster: ptr[uint32](6)}
	mustOK(t, d.AddBindingToEndpointForNode(t.Context(), 123, 1, keep))
	mustOK(t, d.AddBindingToEndpointForNode(t.Context(), 123, 1, gone))
	// The node holds the binding twice, without list ids: both go.
	s.mu2.Lock()
	s.device[1] = []EndpointBindingEntry{{EndpointId: 1, Binding: keep}, {EndpointId: 1, Binding: gone}, {EndpointId: 1, Binding: gone}}
	s.mu2.Unlock()
	var listID uint16
	for _, e := range d.EndpointBindingList() {
		if bindingMatches(e.Binding, gone) {
			listID = e.ListId
		}
	}
	mustOK(t, d.RemoveBindingFromEndpointForNode(t.Context(), listID, 123, 1))
	w := s.lastWrite(t)
	if w.endpoint != 1 || len(w.entries) != 1 || !bindingMatches(w.entries[0].Binding, keep) {
		t.Fatalf("SyncEndpointBindings(%d, %d, %+v)", w.nodeID, w.endpoint, w.entries)
	}
	b := d.EndpointBindingList()
	if len(b) != 1 || !bindingMatches(b[0].Binding, keep) {
		t.Fatalf("EndpointBindingList = %+v", b)
	}
	mustOK(t, d.RemoveBindingFromEndpointForNode(t.Context(), b[0].ListId, 123, 1))
	if w := s.lastWrite(t); len(w.entries) != 0 || w.entries == nil {
		t.Fatalf("the last removal writes the empty list, got %#v", w.entries)
	}
	if b := d.EndpointBindingList(); len(b) != 0 {
		t.Fatalf("EndpointBindingList = %+v", b)
	}
}

// TestEndpointBindingSyncFetchFailureLeavesPending: a fetch that fails is
// the push's error, nothing is written and the entry stays Pending for
// the next RefreshNode — where jf-admin-app writes the one entry over an
// empty list (JFADatastoreSync.cpp:1726-1730 at 6170af84), the recorded
// divergence.
func TestEndpointBindingSyncFetchFailureLeavesPending(t *testing.T) {
	s := newEndpointSync()
	s.fetchErr = errors.New("node unreachable")
	d := nodeWithEndpointSync(t, s)
	err := d.AddBindingToEndpointForNode(t.Context(), 123, 1, BindingTarget{Group: ptr[uint16](10)})
	if !errors.Is(err, ErrSync) || !errors.Is(err, s.fetchErr) {
		t.Fatalf("AddBindingToEndpointForNode = %v", err)
	}
	s.mu2.Lock()
	writes := len(s.writes)
	s.mu2.Unlock()
	if writes != 0 {
		t.Fatalf("%d SyncEndpointBindings calls after a failed fetch", writes)
	}
	b := d.EndpointBindingList()
	if len(b) != 1 || b[0].StatusEntry.State != statePending {
		t.Fatalf("EndpointBindingList = %+v", b)
	}
	// The write's failure leaves the entry as it was too (:1747-1755).
	s.mu2.Lock()
	s.fetchErr, s.writeErr = nil, errors.New("write refused")
	s.mu2.Unlock()
	err = d.RemoveBindingFromEndpointForNode(t.Context(), b[0].ListId, 123, 1)
	if !errors.Is(err, ErrSync) || !errors.Is(err, s.writeErr) {
		t.Fatalf("RemoveBindingFromEndpointForNode = %v", err)
	}
	if b := d.EndpointBindingList(); len(b) != 1 || b[0].StatusEntry.State != stateDeletePending {
		t.Fatalf("EndpointBindingList = %+v", b)
	}
}

// TestEndpointBindingSyncCommandPushesAfterAnswer: the command path takes
// the same merge once the answer is out (Datastore.Wait).
func TestEndpointBindingSyncCommandPushesAfterAnswer(t *testing.T) {
	s := newEndpointSync()
	d := nodeWithEndpointSync(t, s)
	req := dsdef.AddBindingToEndpointForNodeRequest{NodeId: 123, EndpointId: 1, Binding: BindingTarget{Group: ptr[uint16](10)}}
	if st := invoke(t, d, dsdef.CmdAddBindingToEndpointForNode, req); st != im.StatusSuccess {
		t.Fatalf("AddBindingToEndpointForNode = %v", st)
	}
	w := s.lastWrite(t)
	if w.nodeID != 123 || w.endpoint != 1 || len(w.entries) != 1 {
		t.Fatalf("SyncEndpointBindings(%d, %d, %+v)", w.nodeID, w.endpoint, w.entries)
	}
}
