// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/im"
	mstore "thingfabric.dev/go-fabric/store"
)

// ioFailStore fails the reads and writes its flags name with an I/O error
// that is neither ErrFabricNotFound nor ErrIdentityNotFound.
type ioFailStore struct {
	*fakeStore
	failList, failLabel, failGetFabric, failGetIdentity atomic.Bool
}

var errInjectedIO = errors.New("injected store I/O failure")

func (s *ioFailStore) ListFabrics(ctx context.Context) ([]mstore.FabricRecord, error) {
	if s.failList.Load() {
		return nil, errInjectedIO
	}
	return s.fakeStore.ListFabrics(ctx)
}

func (s *ioFailStore) UpdateFabricLabel(ctx context.Context, idx uint8, label string) error {
	if s.failLabel.Load() {
		return errInjectedIO
	}
	return s.fakeStore.UpdateFabricLabel(ctx, idx, label)
}

func (s *ioFailStore) GetFabric(ctx context.Context, idx uint8) (mstore.FabricRecord, error) {
	if s.failGetFabric.Load() {
		return mstore.FabricRecord{}, errInjectedIO
	}
	return s.fakeStore.GetFabric(ctx, idx)
}

func (s *ioFailStore) GetIdentity(ctx context.Context, idx uint8) (mstore.IdentityRecord, error) {
	if s.failGetIdentity.Load() {
		return mstore.IdentityRecord{}, errInjectedIO
	}
	return s.fakeStore.GetIdentity(ctx, idx)
}

// TestUpdateFabricLabelStoreFailureIsFailure (R2-C2-err-1003): a fabric
// list the store cannot read and a label write it fails are the command's
// FAILURE, not a LabelConflict check skipped nor InvalidFabricIndex for a
// fabric that exists, and the label is not written — as RemoveFabric
// answers a store failure (R2-P6-err-1027).
func TestUpdateFabricLabelStoreFailureIsFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &ioFailStore{fakeStore: newFakeStore()}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 1, RootPublicKey: []byte{4, 1}}); err != nil {
		t.Fatal(err)
	}
	label := func() string {
		t.Helper()
		f, err := st.fakeStore.GetFabric(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		return f.Label
	}
	for name, flag := range map[string]*atomic.Bool{"ListFabrics": &st.failList, "UpdateFabricLabel": &st.failLabel} {
		flag.Store(true)
		resp, err := oc.MatterInvoke(fabricCtx(1), 0x09, core.UpdateFabricLabelRequest{Label: "kitchen"})
		flag.Store(false)
		if got := spec.StatusOf(err); got != im.StatusFailure {
			t.Errorf("UpdateFabricLabel with %s failing = %#v, %v (status %v), want FAILURE", name, resp, err, got)
		}
		if l := label(); l != "" {
			t.Errorf("UpdateFabricLabel with %s failing wrote the label %q", name, l)
		}
	}
	// The store working again: the label is written.
	resp, err := oc.MatterInvoke(fabricCtx(1), 0x09, core.UpdateFabricLabelRequest{Label: "kitchen"})
	if nr, ok := resp.(core.NOCResponse); err != nil || !ok || nr.StatusCode != core.NOCStatusOK {
		t.Fatalf("UpdateFabricLabel = %#v, %v, want OK", resp, err)
	}
}

// TestUpdateNOCStoreFailureIsFailure (R2-C2-err-1003): UpdateNOC answers a
// GetFabric or GetIdentity I/O failure on the accessing fabric FAILURE,
// not InvalidFabricIndex, and the fabric row is not changed.
func TestUpdateNOCStoreFailureIsFailure(t *testing.T) {
	t.Parallel()
	for _, which := range []string{"GetFabric", "GetIdentity"} {
		t.Run(which, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := &ioFailStore{fakeStore: newFakeStore()}
			oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{FailSafe: armedFailSafe(0), SupportedFabrics: 5})
			if err != nil {
				t.Fatal(err)
			}
			_, rootPriv, fabricIndex := commissionTestFabric(ctx, t, oc)
			fabCtx := im.WithFabricFilter(ctx, true, fabricIndex)
			nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+1)
			before, err := st.fakeStore.GetFabric(ctx, fabricIndex)
			if err != nil {
				t.Fatal(err)
			}
			flag := &st.failGetFabric
			if which == "GetIdentity" {
				flag = &st.failGetIdentity
			}
			flag.Store(true)
			resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw})
			flag.Store(false)
			if got := spec.StatusOf(err); got != im.StatusFailure {
				t.Errorf("UpdateNOC with %s failing = %#v, %v (status %v), want FAILURE", which, resp, err, got)
			}
			after, err := st.fakeStore.GetFabric(ctx, fabricIndex)
			if err != nil {
				t.Fatal(err)
			}
			if after.NodeID != before.NodeID {
				t.Errorf("UpdateNOC with %s failing changed the node id %#x -> %#x", which, before.NodeID, after.NodeID)
			}
		})
	}
}
