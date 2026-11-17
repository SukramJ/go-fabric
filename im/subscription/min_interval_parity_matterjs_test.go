// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"testing"
	"time"
)

// TestGrantedMaxIntervalIsAtLeastTwoSeconds (review II, R2-P4-imread-11):
// the granted MaxInterval is never below 2 s, whatever MaxIntervalCeiling
// the subscriber asked for, and the reports are sent no faster than every
// 2 s. Mirrors matter.js packages/node/src/node/server/ServerSubscription.ts
// at db021da0: MIN_INTERVAL = Seconds(2) (:73), the configured minimum never
// below it (:111), #genericMaxInterval raising the granted MaxInterval to it
// (:326-343, called with that minimum at :188-192) and #determineSendingIntervals the send interval (:285-290).
func TestGrantedMaxIntervalIsAtLeastTwoSeconds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		floor, ceiling, wantMax uint16
	}{
		{0, 0, 2},
		{0, 1, 2},
		{1, 1, 2},
		{0, 2, 2},
		{0, 3, 3},
		{3, 3, 3},
	} {
		m := newTestManager(Config{})
		sub, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MinIntervalFloor: tc.floor, MaxIntervalCeiling: tc.ceiling})
		if err != nil {
			t.Fatalf("floor %d ceiling %d: %v", tc.floor, tc.ceiling, err)
		}
		if sub.MaxIntervalCeiling != tc.wantMax {
			t.Errorf("floor %d ceiling %d: granted MaxInterval %d s, want %d s", tc.floor, tc.ceiling, sub.MaxIntervalCeiling, tc.wantMax)
		}
		if got := sub.SendInterval(); got < 2*time.Second {
			t.Errorf("floor %d ceiling %d: send interval %v, want at least 2s", tc.floor, tc.ceiling, got)
		}
	}
}
