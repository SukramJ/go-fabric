// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/netinfra"
	tdir "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	"github.com/SukramJ/go-fabric/im"
)

// TestThreadDirectoryPreferredRoundTrip: the preferred network is saved
// through the store on a controller's write and the host's Set and loaded
// at start, as chip keeps it in attribute storage
// (ThreadNetworkDirectoryCluster.cpp:113, :150 at 6170af84); a saved one
// wins over the configuration's, which serves only when the store saved
// none, and a veto's refusal puts the store back.
func TestThreadDirectoryPreferredRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := netinfra.NewMemoryThreadNetworkStore(0)
	mustT(t, store.SaveNetwork(ctx, exPanID1, dataset1))
	mustT(t, store.SaveNetwork(ctx, exPanID2, dataset2))
	s, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: store, PreferredExtendedPanID: exPanID2})
	mustT(t, err)
	if v, _ := s.MatterRead(tdir.AttrPreferredExtendedPanId); !bytes.Equal(asBytes(v), exPanID2) {
		t.Errorf("start without a saved preferred: %X, want the configuration's %X", v, exPanID2)
	}
	mustT(t, s.MatterWrite(caseCtx(), tdir.AttrPreferredExtendedPanId, exPanID1))
	if id, ok, _ := store.LoadPreferred(ctx); !ok || !bytes.Equal(id, exPanID1) {
		t.Errorf("saved preferred %X (%v), want %X", id, ok, exPanID1)
	}
	again, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: store, PreferredExtendedPanID: exPanID2})
	mustT(t, err)
	if v, _ := again.MatterRead(tdir.AttrPreferredExtendedPanId); !bytes.Equal(asBytes(v), exPanID1) {
		t.Errorf("restart: %X, want the saved %X", v, exPanID1)
	}
	mustT(t, again.SetPreferredExtendedPanID(nil))
	if id, ok, _ := store.LoadPreferred(ctx); !ok || id != nil {
		t.Errorf("saved preferred %X (%v) after the host's clear, want null", id, ok)
	}
	mustT(t, again.SetPreferredExtendedPanID(exPanID2))
	again.SetBeforeWrite(func(context.Context, uint32, any) error { return errors.New("no") })
	if err := again.MatterWrite(caseCtx(), tdir.AttrPreferredExtendedPanId, exPanID1); err == nil {
		t.Error("the veto did not refuse the write")
	}
	if id, _, _ := store.LoadPreferred(ctx); !bytes.Equal(id, exPanID2) {
		t.Errorf("saved preferred %X after the refused write, want %X", id, exPanID2)
	}

	// A Set the server refuses in part saves nothing.
	for _, other := range []map[uint32]any{
		{tdir.AttrThreadNetworkTableSize: uint8(1)},
		{tdir.AttrThreadNetworks: nil},
		{0x00FF: uint8(1)}, // no attribute of the cluster
	} {
		values := map[uint32]any{tdir.AttrPreferredExtendedPanId: exPanID1}
		for id, v := range other {
			values[id] = v
		}
		if err := again.SetAttributes(values); err == nil {
			t.Errorf("SetAttributes %v passed", values)
		}
		if id, _, _ := store.LoadPreferred(ctx); !bytes.Equal(id, exPanID2) {
			t.Errorf("saved preferred %X after the refused Set, want %X", id, exPanID2)
		}
	}

	// A saved null: no preferred network, the configuration's ignored.
	null := netinfra.NewMemoryThreadNetworkStore(0)
	mustT(t, null.SaveNetwork(ctx, exPanID1, dataset1))
	mustT(t, null.SavePreferred(ctx, nil))
	n, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: null, PreferredExtendedPanID: exPanID1})
	mustT(t, err)
	if v, _ := n.MatterRead(tdir.AttrPreferredExtendedPanId); v != nil {
		t.Errorf("a saved null served as %X", v)
	}
}

// TestThreadDirectoryUnknownPreferredIsCleared: a saved preferred network
// the store does not hold is cleared, and the clearing saved, as the
// thermostat clears an unknown active handle; the store's failures refuse
// the server or answer FAILURE and store nothing.
func TestThreadDirectoryUnknownPreferredIsCleared(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stale := &failingStore{datasets: [][]byte{dataset1}, preferred: exPanID3, hasPreferred: true}
	c, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: stale})
	mustT(t, err)
	if v, _ := c.MatterRead(tdir.AttrPreferredExtendedPanId); v != nil {
		t.Errorf("an unknown saved preferred served as %X, want null", v)
	}
	if stale.preferred != nil {
		t.Errorf("the store still holds %X, want the clearing saved", stale.preferred)
	}

	if _, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: &failingStore{loadPref: errors.New("disk")}}); !errors.Is(err, netinfra.ErrInvalidThreadNetworks) {
		t.Errorf("LoadPreferred failing: %v, want ErrInvalidThreadNetworks", err)
	}
	failingClear := &failingStore{datasets: [][]byte{dataset1}, preferred: exPanID3, hasPreferred: true, savePref: errors.New("disk")}
	if _, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: failingClear}); !errors.Is(err, netinfra.ErrInvalidThreadNetworks) {
		t.Errorf("the clearing failing: %v, want ErrInvalidThreadNetworks", err)
	}
	broken := &failingStore{datasets: [][]byte{dataset1}, savePref: errors.New("disk")}
	b, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: broken})
	mustT(t, err)
	if err := b.MatterWrite(caseCtx(), tdir.AttrPreferredExtendedPanId, exPanID1); statusOf(err) != im.StatusFailure {
		t.Errorf("SavePreferred failing: %v, want FAILURE", err)
	}
	if err := b.SetPreferredExtendedPanID(exPanID1); statusOf(err) != im.StatusFailure {
		t.Errorf("SavePreferred failing on the host's Set: %v, want FAILURE", err)
	}
	if v, _ := b.MatterRead(tdir.AttrPreferredExtendedPanId); v != nil {
		t.Errorf("preferred %X stored although the store failed", v)
	}
	// An unchanged preferred network calls no store.
	mustT(t, b.SetPreferredExtendedPanID(nil))

	// ReloadNetworks saves the clearing of a preferred network the store
	// dropped, and a failing save refuses the reload.
	store := &failingStore{datasets: [][]byte{dataset1}}
	r, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: store})
	mustT(t, err)
	mustT(t, r.SetPreferredExtendedPanID(exPanID1))
	store.datasets, store.savePref = nil, errors.New("disk")
	if err := r.ReloadNetworks(ctx); err == nil {
		t.Error("a failing save of the clearing passed the reload")
	}
	store.savePref = nil
	mustT(t, r.ReloadNetworks(ctx))
	if store.preferred != nil {
		t.Errorf("the store holds %X after the reload, want null", store.preferred)
	}
}
