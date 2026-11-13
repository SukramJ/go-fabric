// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/groups/MessagingStateTest.ts at
// 862ad75f: the reception state of group messages per source node and key.
//
// matter.js MessagingState.receptionStateFor is the Manager's
// receptionWindow in Go.

import (
	"testing"
)

func TestMessagingStateReceptionStateFor(t *testing.T) {
	t.Parallel()

	t.Run("returns a stable instance per (source node, key)", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		key := []byte{1, 2, 3} // MessagingStateTest.ts:11
		first := m.receptionWindow(key, 1)
		if m.receptionWindow(key, 1) != first {
			t.Error("receptionStateFor(1, key) returned a new instance")
		}
		if m.receptionWindow(key, 2) == first {
			t.Error("receptionStateFor(2, key) returned the instance of node 1")
		}
	})
}
