// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/store"
)

// TestReceptionStateSurvivesForgetAndReload holds the round-3 finding on
// decision 133: a fail-closed GroupKeyManagement path calls ForgetFabric
// and then Load. The replay windows of the key sets that Load restores
// with the same keys are kept across the forget — an unprimed window
// would take the first counter it sees, so a recorded datagram would be
// admitted once — while a key the reload does not bring back loses its
// windows.
func TestReceptionStateSurvivesForgetAndReload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)

	t.Run("the same keys come back", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		recorded := sealGroup(t, op, sid, 0x0101, 7, 100, true, imPayload([]byte("toggle")))
		if msg, err := m.Decode(ctx, recorded); err != nil || msg.Duplicate {
			t.Fatalf("the first message: err %v", err)
		}
		m.ForgetFabric(1)
		if err := m.Load(ctx); err != nil {
			t.Fatal(err)
		}
		msg, err := m.Decode(ctx, recorded)
		if err != nil {
			t.Fatalf("the replay after the reload: %v", err)
		}
		if !msg.Duplicate {
			t.Fatal("a recorded group datagram was admitted again after ForgetFabric and Load restored the same keys")
		}
	})

	t.Run("a key that does not come back loses its windows", func(t *testing.T) {
		t.Parallel()
		m, st := newTestManager(t)
		if msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 7, 100, true, imPayload([]byte("toggle")))); err != nil || msg.Duplicate {
			t.Fatalf("the first message: err %v", err)
		}
		m.ForgetFabric(1)
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x10, EpochKey0: epochKeyOf(0xB2), EpochStart0: 1})
		if err := m.Load(ctx); err != nil {
			t.Fatal(err)
		}
		if got := receptionEntries(m, op); got != 0 {
			t.Fatalf("%d reception windows of a key no key set derives after the reload, want 0", got)
		}
		m.receptionMu.Lock()
		parked := len(m.parkedReception)
		m.receptionMu.Unlock()
		if parked != 0 {
			t.Fatalf("%d fabrics' windows still parked after the reload, want 0", parked)
		}
	})
}
