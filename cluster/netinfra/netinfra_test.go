// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/netinfra"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdir "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	wdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkmanagement"
	"github.com/SukramJ/go-fabric/im"
)

// failingStore is a ThreadNetworkStore whose calls fail.
type failingStore struct {
	load, save, remove, loadPref, savePref error
	datasets                               [][]byte
	preferred                              []byte
	hasPreferred                           bool
}

func (f *failingStore) Capacity() uint8 { return 2 }

func (f *failingStore) LoadNetworks(context.Context) ([][]byte, error) { return f.datasets, f.load }

func (f *failingStore) SaveNetwork(context.Context, []byte, []byte) error { return f.save }

func (f *failingStore) RemoveNetwork(context.Context, []byte) error { return f.remove }

func (f *failingStore) LoadPreferred(context.Context) (id []byte, ok bool, err error) {
	return f.preferred, f.hasPreferred, f.loadPref
}

func (f *failingStore) SavePreferred(_ context.Context, id []byte) error {
	if f.savePref != nil {
		return f.savePref
	}
	f.preferred, f.hasPreferred = id, true
	return nil
}

// TestThreadDirectoryStoreRefusals: a store that fails to load, holds a
// dataset the server would not accept, holds one twice or more than its
// capacity refuses the server; a store's failing save or removal answers
// FAILURE and changes nothing; the in-memory store's own full refusal is
// RESOURCE_EXHAUSTED.
func TestThreadDirectoryStoreRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, store := range map[string]*failingStore{
		"load error":    {load: errors.New("disk")},
		"bad dataset":   {datasets: [][]byte{{0x01}}},
		"duplicate":     {datasets: [][]byte{dataset1, dataset1Updated}},
		"over capacity": {datasets: [][]byte{dataset1, dataset2, dataset3}},
	} {
		if _, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: store}); !errors.Is(err, netinfra.ErrInvalidThreadNetworks) {
			t.Errorf("%s: %v, want ErrInvalidThreadNetworks", name, err)
		}
	}
	if _, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{PreferredExtendedPanID: exPanID1}); err == nil {
		t.Error("a preferred network outside the store was accepted")
	}

	store := &failingStore{datasets: [][]byte{dataset1}, save: errors.New("disk"), remove: errors.New("disk")}
	s, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{Store: store, PreferredExtendedPanID: exPanID1})
	mustT(t, err)
	if _, st := invoke(s, tdir.CmdAddNetwork, tdir.AddNetworkRequest{OperationalDataset: dataset2}); st != im.StatusFailure {
		t.Errorf("failing save: %v, want FAILURE", st)
	}
	mustT(t, s.SetPreferredExtendedPanID(nil))
	if _, st := invoke(s, tdir.CmdRemoveNetwork, tdir.RemoveNetworkRequest{ExtendedPanId: exPanID1}); st != im.StatusFailure {
		t.Errorf("failing removal: %v, want FAILURE", st)
	}
	if v, _ := s.MatterRead(tdir.AttrThreadNetworks); len(v.(spec.List[netinfra.ThreadNetwork])) != 1 {
		t.Errorf("ThreadNetworks %v after the failures, want the one network", v)
	}
	store.save = netinfra.ErrStoreFull
	if _, st := invoke(s, tdir.CmdAddNetwork, tdir.AddNetworkRequest{OperationalDataset: dataset2}); st != im.StatusResourceExhausted {
		t.Errorf("store full: %v, want RESOURCE_EXHAUSTED", st)
	}
	for _, payload := range []any{nil, "x"} {
		for _, cmd := range []uint32{tdir.CmdAddNetwork, tdir.CmdRemoveNetwork, tdir.CmdGetOperationalDataset} {
			if _, st := invoke(s, cmd, payload); st != im.StatusInvalidCommand {
				t.Errorf("command 0x%02X with %v: %v, want INVALID_COMMAND", cmd, payload, st)
			}
		}
	}

	mem := netinfra.NewMemoryThreadNetworkStore(1)
	mustT(t, mem.SaveNetwork(ctx, exPanID1, dataset1))
	if err := mem.SaveNetwork(ctx, exPanID2, dataset2); !errors.Is(err, netinfra.ErrStoreFull) {
		t.Errorf("second network: %v, want ErrStoreFull", err)
	}
	if err := mem.SaveNetwork(ctx, []byte{1}, dataset2); err == nil {
		t.Error("a 1-byte Extended PAN ID was saved")
	}
	mustT(t, mem.RemoveNetwork(ctx, exPanID2))
}

// TestThreadDirectoryReloadAndSet: ReloadNetworks reports a changed
// store and clears a preferred network the store dropped; the typed Set
// takes the preferred network under the write's rule and refuses the
// store's attributes.
func TestThreadDirectoryReloadAndSet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, store := newDirectory(t, 0)
	dirty := watch(s)
	mustT(t, s.ReloadNetworks(ctx))
	if !dirty.empty() {
		t.Error("an unchanged store reported a change")
	}
	storeDirectly(t, s, store, exPanID1, dataset1)
	storeDirectly(t, s, store, exPanID2, dataset2)
	mustT(t, s.SetPreferredExtendedPanID(exPanID1))
	if err := s.SetPreferredExtendedPanID(exPanID3); statusOf(err) != im.StatusConstraintError {
		t.Errorf("unknown preferred: %v, want CONSTRAINT_ERROR", err)
	}
	if err := s.SetAttributes(map[uint32]any{tdir.AttrPreferredExtendedPanId: 7}); statusOf(err) != im.StatusConstraintError {
		t.Errorf("non-octet preferred: %v, want CONSTRAINT_ERROR", err)
	}
	mustT(t, s.SetAttributes(map[uint32]any{tdir.AttrThreadNetworkTableSize: uint8(netinfra.DefaultThreadNetworkCapacity)}))
	if err := s.SetAttributes(map[uint32]any{tdir.AttrThreadNetworkTableSize: uint8(1)}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("TableSize: %v, want ErrNotHeld", err)
	}
	cur, _ := s.MatterRead(tdir.AttrThreadNetworks)
	mustT(t, s.SetAttributes(map[uint32]any{tdir.AttrThreadNetworks: cur}))
	if err := s.SetAttributes(map[uint32]any{tdir.AttrThreadNetworks: spec.List[netinfra.ThreadNetwork]{}}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("ThreadNetworks: %v, want ErrNotHeld", err)
	}
	mustT(t, s.SetAttributes(map[uint32]any{}))
	if err := s.MatterWrite(caseCtx(), tdir.AttrThreadNetworks, nil); err == nil {
		t.Error("ThreadNetworks was written")
	}
	before := s.MatterDataVersion()
	mustT(t, store.RemoveNetwork(ctx, exPanID1))
	dirty.clear()
	mustT(t, s.ReloadNetworks(ctx))
	if !dirty.has(tdir.AttrThreadNetworks) || !dirty.has(tdir.AttrPreferredExtendedPanId) || s.MatterDataVersion() == before {
		t.Error("the reload reported no change")
	}
	if v, _ := s.MatterRead(tdir.AttrPreferredExtendedPanId); v != nil {
		t.Errorf("PreferredExtendedPanID %X after its network left the store, want null", v)
	}
	// A dataset that does not frame: a TLV past the end, the extended
	// length, more than 254 bytes.
	for _, ds := range [][]byte{{0x00, 0x05, 0x01}, {0x00, 0xff, 0x00, 0x01, 0x00}, bytes.Repeat([]byte{0x7f, 0x00}, 128), {0x00}} {
		if _, st := invoke(s, tdir.CmdAddNetwork, tdir.AddNetworkRequest{OperationalDataset: ds}); st != im.StatusConstraintError {
			t.Errorf("dataset % X: %v, want CONSTRAINT_ERROR", ds, st)
		}
	}
	// A Channel on page 1.
	page1 := bytes.Clone(dataset1)
	page1[12] = 1
	if _, st := invoke(s, tdir.CmdAddNetwork, tdir.AddNetworkRequest{OperationalDataset: page1}); st != im.StatusConstraintError {
		t.Errorf("Channel page 1: %v, want CONSTRAINT_ERROR", st)
	}
}

// TestWiFiManagementPlumbing: the attributes are read-only to a
// controller, and the data version moves with the credentials.
func TestWiFiManagementPlumbing(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	before := s.MatterDataVersion()
	mustT(t, s.SetNetworkCredentials(testSSID, testPassphrase))
	if s.MatterDataVersion() == before {
		t.Error("the data version did not move")
	}
	if err := s.MatterWrite(caseCtx(), wdef.AttrSsid, []byte("x")); err == nil {
		t.Error("Ssid was written")
	}
	if err := s.SetAttributes(map[uint32]any{wdef.AttrSsid: testSSID}); err != nil {
		t.Errorf("the served Ssid again: %v", err)
	}
	if err := s.SetAttributes(map[uint32]any{wdef.AttrPassphraseSurrogate: []byte("x")}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("PassphraseSurrogate as bytes: %v, want ErrNotHeld", err)
	}
}
