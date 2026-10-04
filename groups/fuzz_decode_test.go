// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// FuzzDecode feeds arbitrary datagrams to the group receive path. Every
// group message arrives unauthenticated from the network, so Decode must
// neither panic nor accept anything its keys did not seal.
func FuzzDecode(f *testing.F) {
	st := newMemStore()
	st.addFabric(1, 0x0FAB, testCompressed)
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x10, EpochKey0: epochKeyOf(0xA1), EpochStart0: 1})
	st.mapGroup(1, 0x0101, 0x10)
	op, err := OperationalKey(epochKeyOf(0xA1), testCompressed)
	if err != nil {
		f.Fatal(err)
	}
	sid, err := SessionID(op)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(sealGroup(f, op, sid, 0x0101, 1, 1, true, []byte("seed")))
	f.Add(sealGroup(f, op, sid, 0x0101, 1, 2, false, []byte("seed")))
	f.Add([]byte{0x06, 0xEF, 0x5F, 0x81})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, datagram []byte) {
		m, err := NewManager(st, nil)
		if err != nil {
			t.Fatal(err)
		}
		msg, err := m.Decode(context.Background(), datagram)
		if err != nil {
			return
		}
		if msg.FabricIndex != 1 || msg.GroupID == 0 || !msg.Header.HasSourceNodeID {
			t.Fatalf("accepted a malformed message: %+v", msg)
		}
	})
}
