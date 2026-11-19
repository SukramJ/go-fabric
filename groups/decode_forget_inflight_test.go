// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"errors"
	"testing"
)

// A decode whose candidates were taken before ForgetFabric and whose
// reception window is reached after it (candidates releases m.mu before
// completeMessage) is dropped: it neither takes a fresh, unprimed window
// that admits a recorded counter for the forgotten fabric, nor admits a
// counter the reload's parked window then does not know, so the same
// datagram would pass twice (review II, R2-P7-sec-02). The interleaving is
// sequenced by hand, as in TestParkedWindowWinsOverAFreshOne.
func TestDecodeInFlightAcrossForgetIsDropped(t *testing.T) {
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)
	m, _ := newTestManager(t)
	recorded := make([][]byte, 3)
	for i := range recorded {
		recorded[i] = sealGroup(t, op, sid, 0x0101, 7, uint32(100+i), true, imPayload([]byte("toggle")))
		if msg, err := m.Decode(ctx, recorded[i]); err != nil || msg.Duplicate {
			t.Fatalf("message %d: err %v", i, err)
		}
	}
	cands := m.candidates(sid)
	if len(cands) == 0 {
		t.Fatal("no candidate")
	}
	replay, ok := tryDecrypt(recorded[0], sid, true, cands[0])
	if !ok {
		t.Fatal("replay did not decrypt")
	}
	fresh := sealGroup(t, op, sid, 0x0101, 7, 103, true, imPayload([]byte("toggle")))
	freshMsg, ok := tryDecrypt(fresh, sid, true, cands[0])
	if !ok {
		t.Fatal("fresh did not decrypt")
	}
	m.ForgetFabric(1)
	if err := m.completeMessage(replay, cands[0]); !errors.Is(err, errFabricChanged) {
		t.Errorf("replay of counter 100 completing after ForgetFabric: err %v (Duplicate=%v), want errFabricChanged", err, replay.Duplicate)
	}
	if err := m.completeMessage(freshMsg, cands[0]); !errors.Is(err, errFabricChanged) {
		t.Errorf("counter 103 completing after ForgetFabric: err %v, want errFabricChanged", err)
	}
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	// The parked window still holds counter 100; 103 was dropped in
	// flight, so its one delivery is now.
	if msg, err := m.Decode(ctx, recorded[0]); err != nil || !msg.Duplicate {
		t.Errorf("replay of counter 100 after the reload: err %v, want a duplicate", err)
	}
	if msg, err := m.Decode(ctx, fresh); err != nil || msg.Duplicate {
		t.Errorf("counter 103 after the reload: err %v, want admitted once", err)
	}
	if msg, err := m.Decode(ctx, fresh); err != nil || !msg.Duplicate {
		t.Errorf("counter 103 again: err %v, want a duplicate", err)
	}
}
