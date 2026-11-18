// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// TestAFabricThatFailsToLoadLeavesTheOthersServed: when the store cannot
// read one fabric's group state, the first load still loads the others —
// a group message of another fabric decodes — and the failing fabric is
// tried again on its own, paced, until it loads; Load reports its error
// meanwhile (review II, R2-P6-fsm-0315).
func TestAFabricThatFailsToLoadLeavesTheOthersServed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newMemStore()
	st.addFabric(1, 0x0FAB, testCompressed)
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x10, EpochKey0: epochKeyOf(0xA1), EpochStart0: 1})
	st.mapGroup(1, 0x0101, 0x10)
	compressed2 := [8]byte{0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22}
	st.addFabric(2, 0x0FAC, compressed2)
	st.failGetOf = map[uint8]error{2: errStoreDown}
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	op1, sid1 := opKeyOf(t, 0xA1)
	if msg, err := m.Decode(ctx, sealGroup(t, op1, sid1, 0x0101, 7, 1, true, imPayload([]byte("x")))); err != nil || msg.Duplicate {
		t.Fatalf("fabric 1's message while fabric 2 cannot load: err %v", err)
	}
	if err := m.Load(ctx); !errors.Is(err, errStoreDown) {
		t.Fatalf("Load while fabric 2 cannot load: %v, want its error", err)
	}
	st.mu.Lock()
	st.failGetOf = nil
	st.mu.Unlock()
	if err := m.Load(ctx); err != nil {
		t.Fatalf("Load once fabric 2 loads: %v", err)
	}
	m.mu.RLock()
	_, loaded := m.fabrics[2]
	m.mu.RUnlock()
	if !loaded {
		t.Fatal("fabric 2 was not loaded once the store could read it")
	}
}
