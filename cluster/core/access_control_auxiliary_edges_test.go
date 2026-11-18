// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// The edges of the auxiliary ACL's synchronisation and read that no other
// test reaches (R2-P5-tmut-18 rest).

// failingAuxSource is an auxSource whose AuxiliaryACL fails while failing
// is set.
type failingAuxSource struct {
	auxSource
	mu      sync.Mutex
	failing bool
}

func (s *failingAuxSource) AuxiliaryACL(ctx context.Context, fabric uint8) ([]mstore.ACLEntry, error) {
	s.mu.Lock()
	failing := s.failing
	s.mu.Unlock()
	if failing {
		return nil, errors.New("source down")
	}
	return s.auxSource.AuxiliaryACL(ctx, fabric)
}

func auxUpdates(e *gcRecordingEmitter) []core.AuxiliaryAccessUpdatedEvent {
	var out []core.AuxiliaryAccessUpdatedEvent
	for _, ev := range e.take() {
		if u, ok := ev.data.(core.AuxiliaryAccessUpdatedEvent); ok {
			out = append(out, u)
		}
	}
	return out
}

func newAuxACL(t *testing.T, src core.AuxiliaryACLSource) (*core.AccessControl, *gcRecordingEmitter) {
	t.Helper()
	acl, err := core.NewAccessControl(newFakeStore())
	if err != nil {
		t.Fatal(err)
	}
	em := &gcRecordingEmitter{}
	acl.SetMatterEventEmitter(em)
	if src != nil {
		acl.RegisterAuxiliaryProvider(context.Background(), src)
	}
	return acl, em
}

// TestAuxiliaryBatchEndWithoutABeginOpensNothing: an end without a begin
// leaves the batch count at zero, so a later nested batch still syncs once,
// at its outer end.
func TestAuxiliaryBatchEndWithoutABeginOpensNothing(t *testing.T) {
	t.Parallel()
	src := &auxSource{fabrics: []uint8{1}}
	acl, em := newAuxACL(t, src)
	ctx := context.Background()
	acl.EndAuxiliaryBatch(ctx)
	acl.BeginAuxiliaryBatch()
	acl.BeginAuxiliaryBatch()
	src.emit(auxEntry(1, []uint16{1}))
	acl.EndAuxiliaryBatch(ctx)
	if got := auxUpdates(em); len(got) != 0 {
		t.Fatalf("events after the inner end of a nested batch: %+v, want none", got)
	}
	acl.EndAuxiliaryBatch(ctx)
	if got := auxUpdates(em); len(got) != 1 || got[0].FabricIndex != 1 {
		t.Fatalf("events after the outer end: %+v, want one for fabric 1", got)
	}
}

// TestAuxiliaryBatchWithoutASource: a batch on a server without an
// auxiliary source syncs nothing.
func TestAuxiliaryBatchWithoutASource(t *testing.T) {
	t.Parallel()
	acl, em := newAuxACL(t, nil)
	before := acl.MatterDataVersion()
	acl.BeginAuxiliaryBatch()
	acl.EndAuxiliaryBatch(context.Background())
	if acl.MatterDataVersion() != before || len(auxUpdates(em)) != 0 {
		t.Fatal("a batch without an auxiliary source changed the cluster")
	}
}

// TestAuxiliarySyncOnASourceErrorKeepsTheState: a source that fails keeps
// the entries applied before — no event, no DataVersion change — and the
// AuxiliaryAcl read fails rather than answering an empty list.
func TestAuxiliarySyncOnASourceErrorKeepsTheState(t *testing.T) {
	t.Parallel()
	src := &failingAuxSource{auxSource: auxSource{fabrics: []uint8{1}}}
	acl, em := newAuxACL(t, src)
	src.emit(auxEntry(1, []uint16{1}))
	_ = auxUpdates(em)
	before := acl.MatterDataVersion()
	src.mu.Lock()
	src.failing = true
	src.mu.Unlock()
	src.emit(auxEntry(1, []uint16{1}))
	if acl.MatterDataVersion() != before {
		t.Error("a failing source moved the DataVersion")
	}
	if got := auxUpdates(em); len(got) != 0 {
		t.Errorf("a failing source emitted %+v", got)
	}
	if v, ok := acl.MatterRead(0x0007); ok {
		t.Errorf("AuxiliaryAcl with a failing source = %+v, want unreadable", v)
	}
}

// TestAuxiliarySyncWithoutAChange: a signal whose entries did not change
// keeps the DataVersion and emits nothing.
func TestAuxiliarySyncWithoutAChange(t *testing.T) {
	t.Parallel()
	src := &auxSource{fabrics: []uint8{1}}
	acl, em := newAuxACL(t, src)
	src.emit(auxEntry(1, []uint16{1}))
	_ = auxUpdates(em)
	before := acl.MatterDataVersion()
	src.emit(auxEntry(1, []uint16{1}))
	if acl.MatterDataVersion() != before {
		t.Error("an unchanged signal moved the DataVersion")
	}
	if got := auxUpdates(em); len(got) != 0 {
		t.Errorf("an unchanged signal emitted %+v", got)
	}
}

// TestAuxiliaryEventOnlyForAKnownFabric: entries of a fabric the node does
// not hold change nothing anyone is told of.
func TestAuxiliaryEventOnlyForAKnownFabric(t *testing.T) {
	t.Parallel()
	src := &auxSource{fabrics: []uint8{1}}
	_, em := newAuxACL(t, src)
	src.emit(auxEntry(1, []uint16{1}), auxEntry(3, []uint16{1}))
	got := auxUpdates(em)
	if len(got) != 1 || got[0].FabricIndex != 1 {
		t.Fatalf("events %+v, want one for fabric 1", got)
	}
}

// TestAuxiliaryEventAdminNode: the event names the CASE subject of the
// change's request; a PASE request and a request without a subject name
// none.
func TestAuxiliaryEventAdminNode(t *testing.T) {
	t.Parallel()
	base := im.WithFabricFilter(context.Background(), false, 1)
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want uint64 // 0: none
	}{
		{"CASE subject", im.WithSubject(base, 0xA1, nil), 0xA1},
		{"PASE", im.WithAuthModePASE(im.WithSubject(base, 0xA1, nil)), 0},
		{"no subject", base, 0},
	} {
		src := &auxSource{fabrics: []uint8{1}}
		acl, em := newAuxACL(t, src)
		acl.BeginAuxiliaryBatch()
		src.emit(auxEntry(1, []uint16{1}))
		acl.EndAuxiliaryBatch(tc.ctx)
		got := auxUpdates(em)
		if len(got) != 1 {
			t.Fatalf("%s: events %+v, want one", tc.name, got)
		}
		switch a := got[0].AdminNodeID; {
		case tc.want == 0 && a != nil:
			t.Errorf("%s: AdminNodeID %#x, want none", tc.name, *a)
		case tc.want != 0 && (a == nil || *a != tc.want):
			t.Errorf("%s: AdminNodeID %v, want %#x", tc.name, a, tc.want)
		}
	}
}

// TestAuxiliaryACLSplitEdges: a wildcard (empty) subject list stays one
// entry, and a subject list of exactly twice the limit splits into two.
func TestAuxiliaryACLSplitEdges(t *testing.T) {
	t.Parallel()
	src := &auxSource{fabrics: []uint8{1}}
	acl, _ := newAuxACL(t, src)
	wildcard := auxEntry(1, []uint16{1})
	wildcard.Subjects = nil
	src.emit(wildcard, auxEntry(1, []uint16{2}, 1, 2, 3, 4, 5, 6, 7, 8))
	v, ok := acl.MatterRead(0x0007)
	if !ok {
		t.Fatal("AuxiliaryAcl unreadable")
	}
	got := v.([]core.AccessControlAuxiliaryEntryStruct)
	if len(got) != 3 {
		t.Fatalf("AuxiliaryAcl = %d entries, want 3 (the wildcard one, then 4 + 4 subjects): %+v", len(got), got)
	}
	if len(got[0].Entry.Subjects) != 0 || len(got[1].Entry.Subjects) != 4 || len(got[2].Entry.Subjects) != 4 {
		t.Fatalf("AuxiliaryAcl subjects %v %v %v, want [] and 4 + 4", got[0].Entry.Subjects, got[1].Entry.Subjects, got[2].Entry.Subjects)
	}
}
