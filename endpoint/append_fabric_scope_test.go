// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/tlv"
)

// ownedEntry is an entry of a fabric-scoped vendor list that encodes
// itself: field 0 its value, field 254 its owner.
type ownedEntry struct{ V, FabricIndex uint8 }

func (e ownedEntry) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(e.V))
	enc.PutUint(tlv.ContextTag(254), uint64(e.FabricIndex))
	_ = enc.EndContainer()
}

// plainEntry is an entry that does not encode itself.
type plainEntry struct{ V, FabricIndex uint8 }

const (
	appendScopeCluster = 0xFFF1FC02
	appendScopeAttr    = 0x0000
)

// appendScopeServer serves one fabric-scoped vendor list, declared with
// contract.FabricScopeDeclarer, and records the value MatterWrite is handed.
// Its MatterRead returns every fabric's entries.
type appendScopeServer struct {
	mu      sync.Mutex
	list    any
	writes  []any
	written any // the ADD's value, when it reached MatterWrite
}

func (*appendScopeServer) MatterClusterID() uint32 { return appendScopeCluster }
func (s *appendScopeServer) MatterRead(attr uint32) (any, bool) {
	if attr != appendScopeAttr {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list, true
}

func (s *appendScopeServer) MatterWrite(_ context.Context, attr uint32, v any) error {
	if attr == appendScopeAttr {
		s.mu.Lock()
		s.writes = append(s.writes, v)
		s.mu.Unlock()
	}
	return nil
}

func (*appendScopeServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*appendScopeServer) MatterReportable() []uint32                             { return nil }
func (*appendScopeServer) MatterFabricScope(attr uint32) (contract.FabricScope, bool) {
	return contract.FabricScope{}, attr == appendScopeAttr
}

// appendScopeReader is an appendScopeServer that is also a
// contract.FabricScopedReader: its filtered read returns the accessing
// fabric's entries.
type appendScopeReader struct{ *appendScopeServer }

func (r appendScopeReader) MatterReadFiltered(ctx context.Context, attr uint32) (any, bool) {
	filtered, fabric := im.FabricFilterFromContext(ctx)
	v, ok := r.MatterRead(attr)
	if !ok || !filtered {
		return v, ok
	}
	list, isPlain := v.([]plainEntry)
	if !isPlain {
		return v, ok
	}
	out := []plainEntry{}
	for _, e := range list {
		if e.FabricIndex == fabric {
			out = append(out, e)
		}
	}
	return out, true
}

// TestListAppendHandsTheServerTheWritersEntries: a list-append write of a
// fabric-scoped list appends to the accessing fabric's entries — the
// dispatcher hands the server that fabric's entries with the new element,
// whatever the server's read returns, as a write's view of a fabric-scoped
// list is always fabric-filtered. The entry's owner is read from its own
// encoding (field 254); a server whose entries do not encode themselves
// takes an append through its filtered read, and without one the append
// answers FAILURE and nothing is written.
func TestListAppendHandsTheServerTheWritersEntries(t *testing.T) {
	t.Parallel()
	path := concreteAttrPath(2, appendScopeCluster, appendScopeAttr)
	path.ListAppend = true
	// The request comes from fabric 2.
	ctx := im.WithSubject(im.WithTimedInteraction(im.WithFabricFilter(context.Background(), false, 2), false), 0x2222, nil)
	// One interaction, as a controller chunks a list write: a REPLACE, then
	// the ADD. The fake keeps its list as it is and records what each write
	// hands it; the ADD's status and value are the ones looked at.
	write := func(srv *appendScopeServer, as contract.ClusterServer, item any) im.StatusCode {
		t.Helper()
		d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{as}}}))
		d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{caseEntry(2, store.PrivilegeAdminister, nil)}})
		replace := path
		replace.ListAppend = false
		resp := im.HandleWriteRequest(ctx, d, im.WriteRequest{Writes: []im.AttributeWrite{writeOf(replace, item), writeOf(path, item)}})
		if len(resp.Responses) != 2 || resp.Responses[0].Status.Status != im.StatusSuccess {
			t.Fatalf("responses %+v, want the REPLACE applied and the ADD answered", resp.Responses)
		}
		srv.mu.Lock()
		defer srv.mu.Unlock()
		if len(srv.writes) == 2 {
			srv.written = srv.writes[1]
		}
		return resp.Responses[1].Status.Status
	}

	t.Run("entries that encode themselves", func(t *testing.T) {
		t.Parallel()
		srv := &appendScopeServer{list: []ownedEntry{{1, 1}, {2, 2}, {3, 1}}}
		if st := write(srv, srv, []ownedEntry{{9, 2}}); st != im.StatusSuccess {
			t.Fatalf("status %v, want Success", st)
		}
		if got, want := srv.written, []ownedEntry{{2, 2}, {9, 2}}; !slices.Equal(got.([]ownedEntry), want) {
			t.Errorf("MatterWrite was handed %+v, want the writer's entries and the new one %+v", got, want)
		}
	})
	t.Run("an entry without an owner", func(t *testing.T) {
		t.Parallel()
		srv := &appendScopeServer{list: []ownedEntry{{1, 0}, {2, 2}}}
		if st := write(srv, srv, []ownedEntry{{9, 2}}); st != im.StatusFailure {
			t.Errorf("status %v, want Failure", st)
		}
		if srv.written != nil {
			t.Errorf("MatterWrite was handed %+v", srv.written)
		}
	})
	t.Run("entries that do not encode themselves, a filtered reader", func(t *testing.T) {
		t.Parallel()
		srv := &appendScopeServer{list: []plainEntry{{1, 1}, {2, 2}}}
		if st := write(srv, appendScopeReader{srv}, []plainEntry{{9, 2}}); st != im.StatusSuccess {
			t.Fatalf("status %v, want Success", st)
		}
		if got, want := srv.written, []plainEntry{{2, 2}, {9, 2}}; !slices.Equal(got.([]plainEntry), want) {
			t.Errorf("MatterWrite was handed %+v, want %+v", got, want)
		}
	})
	t.Run("entries that do not encode themselves, no filtered reader", func(t *testing.T) {
		t.Parallel()
		srv := &appendScopeServer{list: []plainEntry{{1, 1}, {2, 2}}}
		if st := write(srv, srv, []plainEntry{{9, 2}}); st != im.StatusFailure {
			t.Errorf("status %v, want Failure", st)
		}
		if srv.written != nil {
			t.Errorf("MatterWrite was handed %+v", srv.written)
		}
	})
	t.Run("an empty list, no filtered reader", func(t *testing.T) {
		t.Parallel()
		srv := &appendScopeServer{list: []plainEntry{}}
		if st := write(srv, srv, []plainEntry{{9, 2}}); st != im.StatusSuccess {
			t.Fatalf("status %v, want Success", st)
		}
		if got, want := srv.written, []plainEntry{{9, 2}}; !slices.Equal(got.([]plainEntry), want) {
			t.Errorf("MatterWrite was handed %+v, want %+v", got, want)
		}
	})
}
