// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"bytes"
	"context"
	"encoding/hex"
	"log/slog"
	"strings"
	"testing"
)

// TestReceptionStateIsBoundedPerKey holds decision 133 (N4-bdnotes-2): an
// operational key holds the reception state of at most
// maxGroupSendersPerKey source node ids. A sender beyond it that the table
// does not hold yet is refused — its message dropped, no state created —
// and nobody is evicted, so every held sender keeps its replay protection
// whole; the first refusal of a key is logged at warn, once; another key's
// senders are not counted against this one, and the places come back with
// the key's teardown.
func TestReceptionStateIsBoundedPerKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)
	m, _ := newTestManager(t)
	var logged bytes.Buffer
	m.logger = slog.New(slog.NewTextHandler(&logged, nil))
	first := make([][]byte, maxGroupSendersPerKey)
	for s := range first {
		first[s] = sealGroup(t, op, sid, 0x0101, uint64(s+1), 7, true, []byte("toggle"))
		if msg, err := m.Decode(ctx, first[s]); err != nil || msg.Duplicate {
			t.Fatalf("sender %d of %d: err %v", s+1, maxGroupSendersPerKey, err)
		}
	}
	beyond := uint64(maxGroupSendersPerKey + 1)
	for counter := uint32(1); counter <= 3; counter++ {
		if msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, beyond, counter, true, []byte("toggle"))); err == nil {
			t.Fatalf("sender %d beyond the bound was taken (duplicate %v)", beyond, msg.Duplicate)
		}
	}
	if n := strings.Count(logged.String(), "groups.reception.full"); n != 1 || !strings.Contains(logged.String(), "level=WARN") {
		t.Fatalf("the refusals were logged %d times, want once at warn:\n%s", n, logged.String())
	}
	if got := receptionEntries(m, op); got != maxGroupSendersPerKey {
		t.Fatalf("%d reception windows, want the bound %d", got, maxGroupSendersPerKey)
	}
	// Nobody was evicted: every held sender's recorded message is still a
	// duplicate, and its next counter is new.
	for s := range first {
		if msg, err := m.Decode(ctx, first[s]); err != nil || !msg.Duplicate {
			t.Fatalf("the replay of held sender %d: err %v, want a duplicate", s+1, err)
		}
		if msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, uint64(s+1), 8, true, []byte("toggle"))); err != nil || msg.Duplicate {
			t.Fatalf("the next message of held sender %d: err %v", s+1, err)
		}
	}
	// Another key's table is its own.
	other := []byte{9, 9, 9}
	if _, ok := m.receptionWindow(other, beyond); !ok {
		t.Fatal("a sender under another key was refused for this key's bound")
	}
	// The key's teardown frees its places.
	m.receptionMu.Lock()
	delete(m.reception, hex.EncodeToString(op))
	m.receptionMu.Unlock()
	if msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, beyond, 4, true, []byte("toggle"))); err != nil || msg.Duplicate {
		t.Fatalf("sender %d after the key's state was dropped: err %v", beyond, err)
	}
}
