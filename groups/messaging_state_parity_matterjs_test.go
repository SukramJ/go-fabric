// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/groups/MessagingStateTest.ts at
// 862ad75f: the reception state of group messages per source node and key.
//
// matter.js MessagingState.receptionStateFor is the lookup inside the
// Manager's acceptCounter in Go; windowOf reads the instance it keeps.

import (
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// windowOf returns the reception window the Manager holds for (key,
// source), nil when it holds none.
func windowOf(m *Manager, key []byte, source uint64) *mrp.Window {
	m.receptionMu.Lock()
	defer m.receptionMu.Unlock()
	for _, p := range m.reception[hex.EncodeToString(key)] {
		if p.source == source {
			return p.window
		}
	}
	return nil
}

func TestMessagingStateReceptionStateFor(t *testing.T) {
	t.Parallel()

	t.Run("returns a stable instance per (source node, key)", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		key := []byte{1, 2, 3} // MessagingStateTest.ts:11
		m.acceptCounter(key, 1, 1)
		first := windowOf(m, key, 1)
		m.acceptCounter(key, 1, 2)
		if first == nil || windowOf(m, key, 1) != first {
			t.Error("receptionStateFor(1, key) returned a new instance")
		}
		m.acceptCounter(key, 2, 1)
		if second := windowOf(m, key, 2); second == nil || second == first {
			t.Error("receptionStateFor(2, key) returned the instance of node 1")
		}
	})
}
