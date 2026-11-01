// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/session/SessionManagerTest.ts
// at 862ad75f, the reception-state cases of a key set's removal: matter.js
// keys the replay window on the operational key
// (packages/protocol/src/groups/MessagingState.ts:80-98) and forgets it when
// the last key set deriving that key goes
// (packages/protocol/src/groups/FabricGroups.ts:185-219,
// #forgetUnreferencedReceptionState). Review 2026-11 W2-sec-05.

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

func (s *memStore) removeKeySet(idx uint8, id uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets[idx] = slices.DeleteFunc(s.sets[idx], func(o store.GroupKeySet) bool { return o.GroupKeySetID == id })
}

func TestReceptionStateTeardownParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)
	duplicate := func(t *testing.T, m *Manager, counter uint32) bool {
		t.Helper()
		msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 2, counter, true, []byte("x")))
		if err != nil {
			t.Fatal(err)
		}
		return msg.Duplicate
	}
	windows := func(m *Manager) int {
		m.receptionMu.Lock()
		defer m.receptionMu.Unlock()
		return len(m.reception)
	}

	t.Run("forgets group message reception state when its key set is removed so a reused key re-syncs", func(t *testing.T) {
		t.Parallel()
		m, st := newTestManager(t)
		if duplicate(t, m, 1000) || !duplicate(t, m, 500) {
			t.Fatal("precondition: counter 500 after 1000 must be a replay")
		}
		st.removeKeySet(1, 0x10)
		if err := m.Reload(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if n := windows(m); n != 0 {
			t.Fatalf("a removed key set left %d reception window(s)", n)
		}
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x10, EpochKey0: epochKeyOf(0xA1), EpochStart0: 1})
		if err := m.Reload(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if duplicate(t, m, 500) {
			t.Error("the re-provisioned key did not re-synchronise on its first message")
		}
	})

	t.Run("keeps reception state until the last key set sharing an operational key is removed", func(t *testing.T) {
		t.Parallel()
		m, st := newTestManager(t)
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x11, EpochKey0: epochKeyOf(0xA1), EpochStart0: 1})
		st.mapGroup(1, 0x0101, 0x11) // the group stays decodable after 0x10 goes
		if err := m.Reload(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if duplicate(t, m, 1000) {
			t.Fatal("precondition: the first message was taken for a replay")
		}
		st.removeKeySet(1, 0x10)
		if err := m.Reload(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if !duplicate(t, m, 500) {
			t.Fatal("the window of a key another key set still derives was forgotten")
		}
		st.removeKeySet(1, 0x11)
		if err := m.Reload(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if n := windows(m); n != 0 {
			t.Errorf("the last key set's removal left %d reception window(s)", n)
		}
	})

	t.Run("a forgotten fabric leaves no window", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		if duplicate(t, m, 7) {
			t.Fatal("precondition: the first message was taken for a replay")
		}
		m.ForgetFabric(1)
		if n := windows(m); n != 0 {
			t.Errorf("ForgetFabric left %d reception window(s)", n)
		}
	})
}
