// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// holdACLStoreA1 is a dropACLStore whose next ListACL, once armed, reads
// its answer and then holds the caller until release closes.
type holdACLStoreA1 struct {
	*dropACLStore
	hold             atomic.Bool
	entered, release chan struct{}
}

func (s *holdACLStoreA1) ListACL(ctx context.Context, f uint8) ([]store.ACLEntry, error) {
	rows, err := s.dropACLStore.ListACL(ctx, f)
	if s.hold.CompareAndSwap(true, false) {
		close(s.entered)
		<-s.release
	}
	return rows, err
}

// TestAccessControlReadMarkDoesNotOutliveADropA1 (R5-conc-5004, decision
// 681): an Acl read that saw a fabric's rows, held by the store while the
// fabric is removed and its drop runs to the end, does not mark the
// fabric again behind the drop — the drop advanced the fabric's drop
// epoch the read noted before its list: a second drop of the fabric
// reports nothing. The read does not hold writeMu, so the drop does not
// wait for it.
func TestAccessControlReadMarkDoesNotOutliveADropA1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const f uint8 = 3
	st := &holdACLStoreA1{
		dropACLStore: &dropACLStore{acl: map[uint8][]store.ACLEntry{f: {dropAdmin(f)}}, fabrics: []uint8{f}},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	a, err := NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	heard := threadChangeListener(t, a)

	st.hold.Store(true)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		a.MatterReadFiltered(im.WithFabricFilter(ctx, true, f), accessControlAttrACL)
	}()
	<-st.entered // the read holds the rows it saw
	st.removeFabric(f)
	dropDone := make(chan struct{})
	go func() {
		defer close(dropDone)
		_ = a.MatterDropFabric(ctx, f)
	}()
	<-dropDone // the drop ran while the read held its rows
	close(st.release)
	<-readDone
	if got := heard.take(); !slices.Equal(got, []uint32{accessControlAttrACL}) {
		t.Fatalf("the drop reported %v, want [Acl]", got)
	}

	v0 := a.MatterDataVersion()
	if err := a.MatterDropFabric(ctx, f); err != nil {
		t.Fatal(err)
	}
	if got := heard.take(); len(got) != 0 {
		t.Errorf("a second drop of the removed fabric reported %v: the read marked it again after the first drop", got)
	}
	if v := a.MatterDataVersion(); v != v0 {
		t.Errorf("a second drop moved the DataVersion %d → %d", v0, v)
	}
}

// TestAccessControlLocalEntryMarksItsFabricA1 (R5-par-5003): an entry the
// node itself appends to a fabric that held no Acl rows, reported with
// the fabric-taking NotifyLocalEntryInstalledOn, marks the fabric: its
// removal is reported as the Acl shrinking, and the DataVersion moved at
// the notification.
func TestAccessControlLocalEntryMarksItsFabricA1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const f uint8 = 3
	st := &dropACLStore{acl: map[uint8][]store.ACLEntry{1: {dropAdmin(1)}}, fabrics: []uint8{1, f}}
	a, err := NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	heard := threadChangeListener(t, a)
	// The node's own Operate entry, as node/local_acl.go appends it.
	st.acl[f] = append(st.acl[f], store.ACLEntry{FabricIndex: f, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeCASE})
	v0 := a.MatterDataVersion()
	a.NotifyLocalEntryInstalledOn(ctx, f)
	if a.MatterDataVersion() == v0 {
		t.Error("the local entry did not move the DataVersion")
	}
	st.removeFabric(f)
	if err := a.MatterDropFabric(ctx, f); err != nil {
		t.Fatal(err)
	}
	if got := heard.take(); !slices.Equal(got, []uint32{accessControlAttrACL}) {
		t.Errorf("the removal of a fabric holding only the node's own entry reported %v, want [Acl]", got)
	}
}
