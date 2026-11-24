// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	mstore "thingfabric.dev/go-fabric/store"
)

// holdReadStoreA1 is a fakeStore whose next ListFabrics or GetFabric, once
// armed, reads its answer and then holds the caller until release closes:
// a command held between its check and its write.
type holdReadStoreA1 struct {
	*fakeStore
	holdList, holdGet atomic.Bool
	entered, release  chan struct{}
}

func newHoldReadStoreA1() *holdReadStoreA1 {
	return &holdReadStoreA1{fakeStore: newFakeStore(), entered: make(chan struct{}), release: make(chan struct{})}
}

func (s *holdReadStoreA1) hold() {
	close(s.entered)
	<-s.release
}

func (s *holdReadStoreA1) ListFabrics(ctx context.Context) ([]mstore.FabricRecord, error) {
	recs, err := s.fakeStore.ListFabrics(ctx)
	if s.holdList.CompareAndSwap(true, false) {
		s.hold()
	}
	return recs, err
}

func (s *holdReadStoreA1) GetFabric(ctx context.Context, idx uint8) (mstore.FabricRecord, error) {
	rec, err := s.fakeStore.GetFabric(ctx, idx)
	if s.holdGet.CompareAndSwap(true, false) {
		s.hold()
	}
	return rec, err
}

// invokeA1 runs one command in its own goroutine; done closes when it
// returned.
type invokeA1 struct {
	resp any
	err  error
	done chan struct{}
}

func goInvokeA1(oc *core.OperationalCredentials, fabric uint8, cmd uint32, req any) *invokeA1 {
	r := &invokeA1{done: make(chan struct{})}
	go func() {
		defer close(r.done)
		r.resp, r.err = oc.MatterInvoke(fabricCtx(fabric), cmd, req)
	}()
	return r
}

// TestUpdateFabricLabelCheckAndWriteAreOneStepA1 (R4-conc-4053): two
// fabrics relabelled to the same label at once — the first held by the
// store between its LabelConflict check and its write — end with one
// label written and one LabelConflict, never two fabrics carrying the
// label. Not parallel: WaitParkedOnMutexA1 reads every goroutine's stack.
func TestUpdateFabricLabelCheckAndWriteAreOneStepA1(t *testing.T) {
	ctx := context.Background()
	st := newHoldReadStoreA1()
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{1, 2} {
		if _, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: id, RootPublicKey: []byte{4, byte(id)}}); err != nil {
			t.Fatal(err)
		}
	}
	const label = "kitchen"
	st.holdList.Store(true)
	first := goInvokeA1(oc, 1, 0x09, core.UpdateFabricLabelRequest{Label: label})
	<-st.entered
	second := goInvokeA1(oc, 2, 0x09, core.UpdateFabricLabelRequest{Label: label})
	core.WaitParkedOnMutexA1(t, second.done, "handleUpdateFabricLabel")
	close(st.release)
	<-first.done
	<-second.done

	statuses := map[uint8]int{}
	for _, r := range []*invokeA1{first, second} {
		noc, ok := r.resp.(core.NOCResponse)
		if r.err != nil || !ok {
			t.Fatalf("UpdateFabricLabel = %#v, %v; want a NOCResponse", r.resp, r.err)
		}
		statuses[noc.StatusCode]++
	}
	if statuses[core.NOCStatusOK] != 1 || statuses[core.NOCStatusLabelConflict] != 1 {
		t.Errorf("two concurrent relabels to %q answered %v, want one OK and one LabelConflict", label, statuses)
	}
	fabrics, err := st.fakeStore.ListFabrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	carrying := 0
	for i := range fabrics {
		if fabrics[i].Label == label {
			carrying++
		}
	}
	if carrying != 1 {
		t.Errorf("%d fabrics carry the label %q, want 1", carrying, label)
	}
}

// TestSetVIDVerificationStatementReadAndWriteAreOneStepA1 (R3-conc-3056):
// two SetVIDVerificationStatement commands of one fabric at once — one
// setting the statement, held by the store between its read of the
// fabric's VID data and its write, the other setting the VVSC — leave
// both fields set: the second command's VVSC is not overwritten with the
// value the first one read. Not parallel: WaitParkedOnMutexA1 reads every
// goroutine's stack.
func TestSetVIDVerificationStatementReadAndWriteAreOneStepA1(t *testing.T) {
	ctx := context.Background()
	st := newHoldReadStoreA1()
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	idx, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 1, RootPublicKey: []byte{4, 1}})
	if err != nil {
		t.Fatal(err)
	}
	// A fabric without an ICAC may carry a VVSC.
	if err := st.UpsertIdentity(ctx, mstore.IdentityRecord{FabricIndex: idx, NOC: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	statement := bytes.Repeat([]byte{0x5A}, 85)
	vvsc := []byte{0x30, 0x01, 0x02}
	st.holdGet.Store(true)
	first := goInvokeA1(oc, idx, 0x0C, core.SetVidVerificationStatementRequest{VidVerificationStatement: statement, HasVidVerificationStatement: true})
	<-st.entered
	second := goInvokeA1(oc, idx, 0x0C, core.SetVidVerificationStatementRequest{Vvsc: vvsc, HasVvsc: true})
	core.WaitParkedOnMutexA1(t, second.done, "handleSetVidVerificationStatement")
	close(st.release)
	<-first.done
	<-second.done
	for _, r := range []*invokeA1{first, second} {
		if r.err != nil {
			t.Fatalf("SetVIDVerificationStatement: %v", r.err)
		}
	}
	f, err := st.fakeStore.GetFabric(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.VIDVerificationStatement, statement) {
		t.Errorf("statement = %x, want the first command's", f.VIDVerificationStatement)
	}
	if !bytes.Equal(f.VVSC, vvsc) {
		t.Errorf("VVSC = %x, want the second command's %x", f.VVSC, vvsc)
	}
}
