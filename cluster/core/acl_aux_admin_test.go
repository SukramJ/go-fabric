// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// deferAuxSource is an auxiliary ACL source whose entries a test sets and
// whose change it signals.
type deferAuxSource struct {
	mu      sync.Mutex
	entries []mstore.ACLEntry
	fn      func(context.Context, uint8)
}

func (s *deferAuxSource) AuxiliaryACL(context.Context, uint8) ([]mstore.ACLEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]mstore.ACLEntry(nil), s.entries...), nil
}
func (s *deferAuxSource) Fabrics(context.Context) ([]uint8, error) { return []uint8{1, 2}, nil }
func (s *deferAuxSource) OnGroupcastChanged(fn func(context.Context, uint8)) {
	s.mu.Lock()
	s.fn = fn
	s.mu.Unlock()
}

func (s *deferAuxSource) add(ctx context.Context, fabric uint8, signal bool) {
	s.mu.Lock()
	s.entries = append(s.entries, mstore.ACLEntry{FabricIndex: fabric, Privilege: mstore.PrivilegeView, AuthMode: mstore.AuthModeGroup, Subjects: []uint64{uint64(fabric)}})
	fn := s.fn
	s.mu.Unlock()
	if signal {
		fn(ctx, fabric)
	}
}

type deferAuxEvents struct {
	mu  sync.Mutex
	got map[uint8]*uint64
}

func (e *deferAuxEvents) MatterEmitEvent(_ uint16, _, _ uint32, data any, _ contract.EventPriority) {
	if ev, ok := data.(core.AuxiliaryAccessUpdatedEvent); ok {
		e.mu.Lock()
		e.got[ev.FabricIndex] = ev.AdminNodeID
		e.mu.Unlock()
	}
}

// A change of fabric 2 signalled while a Groupcast command of fabric 1
// holds the auxiliary batch open: fabric 2's AuxiliaryAccessUpdated names
// the node of fabric 2's own request, never fabric 1's administrator
// (review II, R2-P6-conc-1007).
func TestAuxiliaryEventOfADeferredChangeNamesItsOwnNode(t *testing.T) {
	t.Parallel()
	ac, err := core.NewAccessControl(newFakeStore())
	if err != nil {
		t.Fatal(err)
	}
	ev := &deferAuxEvents{got: map[uint8]*uint64{}}
	ac.SetMatterEventEmitter(ev)
	src := &deferAuxSource{}
	ac.RegisterAuxiliaryProvider(context.Background(), src)
	const adminA, adminB = 0xA1, 0xB2
	ctxA := im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), adminA, nil)
	ctxB := im.WithSubject(im.WithFabricFilter(context.Background(), false, 2), adminB, nil)
	ac.BeginAuxiliaryBatch()
	src.add(ctxB, 2, true)  // another source's change of fabric 2, deferred
	src.add(ctxA, 1, false) // the batch's own change of fabric 1
	ac.EndAuxiliaryBatch(ctxA)
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if a := ev.got[1]; a == nil || *a != adminA {
		t.Errorf("fabric 1's event names %v, want the closing command's node %#x", a, adminA)
	}
	if a := ev.got[2]; a == nil || *a != adminB {
		t.Errorf("fabric 2's event names %v, want its own change's node %#x", a, adminB)
	}
}

// A change signalled without a request — GroupKeyManagement's retry of a
// stale group state — while a Groupcast command's batch is open does not
// displace the context the command's own change recorded: the event still
// names the command's node (review II, R2-P6-err-1701 R3-1).
func TestAuxiliaryChangeWithoutARequestKeepsTheBatchsNode(t *testing.T) {
	t.Parallel()
	ac, err := core.NewAccessControl(newFakeStore())
	if err != nil {
		t.Fatal(err)
	}
	ev := &deferAuxEvents{got: map[uint8]*uint64{}}
	ac.SetMatterEventEmitter(ev)
	src := &deferAuxSource{}
	ac.RegisterAuxiliaryProvider(context.Background(), src)
	const adminA = 0xA1
	ctxA := im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), adminA, nil)
	ac.BeginAuxiliaryBatch()
	src.add(ctxA, 1, true)                 // the command's own change
	src.add(context.Background(), 1, true) // a retry's notification
	ac.EndAuxiliaryBatch(ctxA)
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if a := ev.got[1]; a == nil || *a != adminA {
		t.Errorf("fabric 1's event names %v, want the command's node %#x", a, adminA)
	}
}
