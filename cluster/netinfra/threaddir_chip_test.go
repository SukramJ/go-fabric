// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/netinfra"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdir "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	"github.com/SukramJ/go-fabric/im"
)

// The chip cases of src/app/clusters/thread-network-directory-server/tests/
// TestThreadNetworkDirectoryCluster.cpp at 6170af84 (:123-507), ported by
// meaning: chip's FakeThreadNetworkDirectoryStorage (capacity 4 by
// default) is a MemoryThreadNetworkStore, a network the chip case adds
// "directly to storage" is saved to the store and taken in with
// ReloadNetworks — this server serves its table, chip reads the storage
// on every read (ThreadNetworkDirectoryCluster.cpp:153-183). The
// migration case (:514) tests chip's SafeAttributePersistenceProvider
// migration, a storage format this module never had, and is left out.

// tlvSuffix is every sub-TLV of chip's kDataset1 after the Extended PAN
// ID: Mesh-Local Prefix, Network Key, "OpenThread-5938", PAN ID 0x5938,
// PSKc and Security Policy (:54-59).
var tlvSuffix = []byte{
	0x07, 0x08, 0xfd, 0xf1, 0xf1, 0xad, 0xd0, 0x79, 0x7d, 0xc0,
	0x05, 0x10, 0xf3, 0x66, 0xce, 0xc7, 0xa4, 0x46, 0xba, 0xb9, 0x78, 0xd9, 0x0d, 0x27, 0xab, 0xe3, 0x8f, 0x23,
	0x03, 0x0f, 0x4f, 0x70, 0x65, 0x6e, 0x54, 0x68, 0x72, 0x65, 0x61, 0x64, 0x2d, 0x35, 0x39, 0x33, 0x38,
	0x01, 0x02, 0x59, 0x38,
	0x04, 0x10, 0x3c, 0xa6, 0x7c, 0x96, 0x9e, 0xfb, 0x0d, 0x0c, 0x74, 0xa4, 0xd8, 0xee, 0x92, 0x3b, 0x57, 0x6c,
	0x0c, 0x04, 0x02, 0xa0, 0xf7, 0xf8,
}

// dataset is chip's test dataset with Active Timestamp ts and Extended
// PAN ID extPanID, Channel 15, Channel Mask 0x07fff800 (:49-92).
func dataset(ts byte, extPanID []byte) []byte {
	out := []byte{0x0e, 0x08, 0, 0, 0, 0, 0, 0, 0, ts, 0x00, 0x03, 0x00, 0x00, 0x0f, 0x35, 0x04, 0x07, 0xff, 0xf8, 0x00, 0x02, 0x08}
	out = append(out, extPanID...)
	return append(out, tlvSuffix...)
}

var (
	exPanID1 = []byte{0x39, 0x75, 0x8e, 0xc8, 0x14, 0x4b, 0x07, 0xfb} // :95
	exPanID2 = []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22} // :96
	exPanID3 = bytes.Repeat([]byte{0x33}, 8)                          // :97
	exPanID4 = bytes.Repeat([]byte{0x44}, 8)                          // :330
	exPanID5 = bytes.Repeat([]byte{0x55}, 8)                          // :335-342

	dataset1        = dataset(1, exPanID1) // kDataset1
	dataset1Updated = dataset(2, exPanID1) // kDataset1Updated
	dataset2        = dataset(1, exPanID2) // kDataset2
	dataset3        = dataset(1, exPanID3) // kDataset3
)

func newDirectory(t *testing.T, capacity uint8) (*netinfra.ThreadDirectoryServer, *netinfra.MemoryThreadNetworkStore) {
	t.Helper()
	store := netinfra.NewMemoryThreadNetworkStore(capacity)
	s, err := netinfra.NewThreadDirectory(context.Background(), netinfra.ThreadDirectoryConfig{Store: store})
	mustT(t, err)
	return s, store
}

// storeDirectly is chip's `storage.AddOrUpdateNetwork` beside the cluster.
func storeDirectly(t *testing.T, s *netinfra.ThreadDirectoryServer, store *netinfra.MemoryThreadNetworkStore, extPanID, ds []byte) {
	t.Helper()
	mustT(t, store.SaveNetwork(context.Background(), extPanID, ds))
	mustT(t, s.ReloadNetworks(context.Background()))
}

func invoke(s *netinfra.ThreadDirectoryServer, cmd uint32, req any) (any, im.StatusCode) {
	resp, err := s.MatterInvoke(caseCtx(), cmd, req)
	return resp, statusOf(err)
}

func contains(t *testing.T, store *netinfra.MemoryThreadNetworkStore, extPanID []byte) bool {
	t.Helper()
	datasets, err := store.LoadNetworks(context.Background())
	mustT(t, err)
	for _, ds := range datasets {
		if bytes.Contains(ds, append([]byte{0x02, 0x08}, extPanID...)) {
			return true
		}
	}
	return false
}

// TestThreadNetworkDirectoryCluster_TestMetadata is :123-145.
func TestThreadNetworkDirectoryCluster_TestMetadata(t *testing.T) {
	t.Parallel()
	s, _ := newDirectory(t, 0)
	if got, want := clusterAttributes(s), []uint32{tdir.AttrPreferredExtendedPanId, tdir.AttrThreadNetworks, tdir.AttrThreadNetworkTableSize}; !slices.Equal(got, want) {
		t.Errorf("attributes %v, want %v", got, want)
	}
	if got, want := s.MatterAcceptedCommands(), []uint32{tdir.CmdAddNetwork, tdir.CmdRemoveNetwork, tdir.CmdGetOperationalDataset}; !slices.Equal(got, want) {
		t.Errorf("AcceptedCommandList %v, want %v", got, want)
	}
	if got := s.MatterGeneratedCommands(); !slices.Equal(got, []uint32{tdir.CmdOperationalDatasetResponse}) {
		t.Errorf("GeneratedCommandList %v", got)
	}
	if fm, _ := s.MatterRead(0xFFFC); fm != uint32(0) {
		t.Errorf("FeatureMap %v, want 0 (:89-90)", fm)
	}
}

// TestThreadNetworkDirectoryCluster_TestReadTableSize is :150-159.
func TestThreadNetworkDirectoryCluster_TestReadTableSize(t *testing.T) {
	t.Parallel()
	s, _ := newDirectory(t, 3)
	if v, _ := s.MatterRead(tdir.AttrThreadNetworkTableSize); v != uint8(3) {
		t.Errorf("ThreadNetworkTableSize %v, want 3", v)
	}
	d, _ := newDirectory(t, 0)
	if v, _ := d.MatterRead(tdir.AttrThreadNetworkTableSize); v != uint8(netinfra.DefaultThreadNetworkCapacity) {
		t.Errorf("default ThreadNetworkTableSize %v, want %d", v, netinfra.DefaultThreadNetworkCapacity)
	}
}

// TestThreadNetworkDirectoryCluster_TestPreferredExtendedPanId is :165-218,
// with the zero-length write chip reads as null
// (ThreadNetworkDirectoryCluster.cpp:145-146).
func TestThreadNetworkDirectoryCluster_TestPreferredExtendedPanId(t *testing.T) {
	t.Parallel()
	s, store := newDirectory(t, 0)
	ctx := caseCtx()
	if v, _ := s.MatterRead(tdir.AttrPreferredExtendedPanId); v != nil {
		t.Errorf("initial PreferredExtendedPanID %v, want null", v)
	}
	if err := s.MatterWrite(ctx, tdir.AttrPreferredExtendedPanId, exPanID1); statusOf(err) != im.StatusConstraintError {
		t.Errorf("write of an unknown network: %v, want CONSTRAINT_ERROR", err)
	}
	storeDirectly(t, s, store, exPanID1, dataset1)
	mustT(t, s.MatterWrite(ctx, tdir.AttrPreferredExtendedPanId, exPanID1))
	if v, _ := s.MatterRead(tdir.AttrPreferredExtendedPanId); !bytes.Equal(asBytes(v), exPanID1) {
		t.Errorf("PreferredExtendedPanID %X, want %X", v, exPanID1)
	}
	mustT(t, s.MatterWrite(ctx, tdir.AttrPreferredExtendedPanId, nil))
	if v, _ := s.MatterRead(tdir.AttrPreferredExtendedPanId); v != nil {
		t.Errorf("PreferredExtendedPanID %v after the null write, want null", v)
	}
	mustT(t, s.MatterWrite(ctx, tdir.AttrPreferredExtendedPanId, exPanID1))
	mustT(t, s.MatterWrite(ctx, tdir.AttrPreferredExtendedPanId, []byte{}))
	if v, _ := s.MatterRead(tdir.AttrPreferredExtendedPanId); v != nil {
		t.Errorf("PreferredExtendedPanID %v after the empty write, want null", v)
	}
	if err := s.MatterWrite(ctx, tdir.AttrPreferredExtendedPanId, []byte{1, 2, 3}); statusOf(err) != im.StatusConstraintError {
		t.Errorf("write of 3 bytes: %v, want CONSTRAINT_ERROR", err)
	}
}

func asBytes(v any) []byte { b, _ := v.([]byte); return b }

// TestThreadNetworkDirectoryCluster_TestThreadNetworksList is :224-261.
func TestThreadNetworkDirectoryCluster_TestThreadNetworksList(t *testing.T) {
	t.Parallel()
	s, store := newDirectory(t, 0)
	if v, _ := s.MatterRead(tdir.AttrThreadNetworks); len(v.(spec.List[netinfra.ThreadNetwork])) != 0 {
		t.Errorf("initial ThreadNetworks %v, want empty", v)
	}
	storeDirectly(t, s, store, exPanID1, dataset1)
	storeDirectly(t, s, store, exPanID2, dataset2)
	v, _ := s.MatterRead(tdir.AttrThreadNetworks)
	list := v.(spec.List[netinfra.ThreadNetwork])
	if len(list) != 2 {
		t.Fatalf("ThreadNetworks %v, want two entries", list)
	}
	for _, n := range list {
		if len(n.ExtendedPanId) != 8 || n.NetworkName == "" || n.Channel == 0 {
			t.Errorf("entry %+v", n)
		}
	}
	want := netinfra.ThreadNetwork{ExtendedPanId: exPanID1, NetworkName: "OpenThread-5938", Channel: 15, ActiveTimestamp: 1}
	if n := list[0]; !bytes.Equal(n.ExtendedPanId, want.ExtendedPanId) || n.NetworkName != want.NetworkName ||
		n.Channel != want.Channel || n.ActiveTimestamp != want.ActiveTimestamp {
		t.Errorf("first entry %+v, want %+v", n, want)
	}
}

// TestThreadNetworkDirectoryCluster_TestAddNetworkCommand is :267-354.
func TestThreadNetworkDirectoryCluster_TestAddNetworkCommand(t *testing.T) {
	t.Parallel()
	s, store := newDirectory(t, 4) // the fake storage's default capacity
	dirty := watch(s)
	add := func(ds []byte) im.StatusCode {
		_, st := invoke(s, tdir.CmdAddNetwork, tdir.AddNetworkRequest{OperationalDataset: ds})
		return st
	}
	if st := add(nil); st != im.StatusConstraintError {
		t.Errorf("empty dataset: %v, want CONSTRAINT_ERROR", st)
	}
	// Beyond chip: an empty Network Name, which ThreadNetworks cannot
	// serve ("1 to 16"), and the one-byte Channel chip's getter refuses
	// (ThreadOperationalDataset.cpp:206).
	unnamed := slices.Concat(dataset1[:len(dataset1)-len(tlvSuffix)], tlvSuffix[:28], []byte{0x03, 0x00}, tlvSuffix[45:])
	if st := add(unnamed); st != im.StatusConstraintError {
		t.Errorf("empty Network Name: %v, want CONSTRAINT_ERROR", st)
	}
	oneByteChannel := slices.Concat(dataset1[:10], []byte{0x00, 0x01, 0x0f}, dataset1[15:])
	if st := add(oneByteChannel); st != im.StatusConstraintError {
		t.Errorf("one-byte Channel: %v, want CONSTRAINT_ERROR", st)
	}
	if !dirty.empty() {
		t.Error("a refused dataset reported a change")
	}
	if st := add(dataset1); st != im.StatusSuccess {
		t.Errorf("valid dataset: %v, want SUCCESS", st)
	}
	if !contains(t, store, exPanID1) || !dirty.has(tdir.AttrThreadNetworks) {
		t.Error("the network is not stored or ThreadNetworks not reported")
	}
	if st := add(dataset1); st != im.StatusInvalidInState {
		t.Errorf("same Active Timestamp: %v, want INVALID_IN_STATE", st)
	}
	if st := add(dataset1Updated); st != im.StatusSuccess {
		t.Errorf("newer Active Timestamp: %v, want SUCCESS", st)
	}
	storeDirectly(t, s, store, exPanID2, dataset2)
	storeDirectly(t, s, store, exPanID3, dataset3)
	storeDirectly(t, s, store, exPanID4, dataset(1, exPanID4))
	if st := add(dataset(1, exPanID5)); st != im.StatusResourceExhausted {
		t.Errorf("full table: %v, want RESOURCE_EXHAUSTED", st)
	}
}

// TestThreadNetworkDirectoryCluster_TestRemoveNetworkCommand is :360-430.
func TestThreadNetworkDirectoryCluster_TestRemoveNetworkCommand(t *testing.T) {
	t.Parallel()
	s, store := newDirectory(t, 0)
	storeDirectly(t, s, store, exPanID1, dataset1)
	remove := func(id []byte) im.StatusCode {
		_, st := invoke(s, tdir.CmdRemoveNetwork, tdir.RemoveNetworkRequest{ExtendedPanId: id})
		return st
	}
	if st := remove([]byte{1, 2, 3}); st != im.StatusConstraintError {
		t.Errorf("3-byte id: %v, want CONSTRAINT_ERROR", st)
	}
	if st := remove(exPanID2); st != im.StatusNotFound {
		t.Errorf("unknown network: %v, want NOT_FOUND", st)
	}
	mustT(t, s.MatterWrite(caseCtx(), tdir.AttrPreferredExtendedPanId, exPanID1))
	if st := remove(exPanID1); st != im.StatusConstraintError {
		t.Errorf("preferred network: %v, want CONSTRAINT_ERROR", st)
	}
	mustT(t, s.MatterWrite(caseCtx(), tdir.AttrPreferredExtendedPanId, nil))
	if st := remove(exPanID1); st != im.StatusSuccess {
		t.Errorf("remove: %v, want SUCCESS", st)
	}
	if contains(t, store, exPanID1) {
		t.Error("the network survives RemoveNetwork")
	}
}

// TestThreadNetworkDirectoryCluster_TestGetOperationalDatasetCommand is
// :437-507.
func TestThreadNetworkDirectoryCluster_TestGetOperationalDatasetCommand(t *testing.T) {
	t.Parallel()
	s, store := newDirectory(t, 0)
	storeDirectly(t, s, store, exPanID1, dataset1)
	get := func(ctx context.Context, id []byte) (any, im.StatusCode) {
		resp, err := s.MatterInvoke(ctx, tdir.CmdGetOperationalDataset, tdir.GetOperationalDatasetRequest{ExtendedPanId: id})
		return resp, statusOf(err)
	}
	if _, st := get(groupCtx(), exPanID1); st != im.StatusUnsupportedAccess {
		t.Errorf("group: %v, want UNSUPPORTED_ACCESS", st)
	}
	if _, st := get(paseCtx(), exPanID1); st != im.StatusUnsupportedAccess {
		t.Errorf("PASE: %v, want UNSUPPORTED_ACCESS", st)
	}
	if _, st := get(caseCtx(), []byte{1, 2}); st != im.StatusConstraintError {
		t.Errorf("2-byte id: %v, want CONSTRAINT_ERROR", st)
	}
	if _, st := get(caseCtx(), exPanID2); st != im.StatusNotFound {
		t.Errorf("unknown network: %v, want NOT_FOUND", st)
	}
	resp, st := get(caseCtx(), exPanID1)
	r, ok := resp.(tdir.OperationalDatasetResponse)
	if st != im.StatusSuccess || !ok || !bytes.Equal(r.OperationalDataset, dataset1) {
		t.Errorf("response %#v (%v), want the stored dataset", resp, st)
	}
}
