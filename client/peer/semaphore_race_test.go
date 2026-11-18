// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestSemaphoreSlotTimeoutRacesNoGrant: a slot whose timeout fires before
// the grant has returned releases it without a data race on the slot's
// timer (review II, R2-P6-conc-2103); run under -race.
func TestSemaphoreSlotTimeoutRacesNoGrant(t *testing.T) {
	t.Parallel()
	s := NewSemaphore(nil, "race", 4, 0, time.Nanosecond)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				slot, err := s.Obtain(context.Background())
				if err != nil {
					t.Error(err)
					return
				}
				slot.Close()
			}
		})
	}
	wg.Wait()
	// Every Close has returned, so every release is done (sync.Once).
	if n := s.Running(); n != 0 {
		t.Fatalf("%d slots still running", n)
	}
}
