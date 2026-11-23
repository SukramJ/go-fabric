// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/icd"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// counterWatchStore counts the counter saves once watch is set.
type counterWatchStore struct {
	*memstore.Store
	watch atomic.Bool
	saves atomic.Int32
}

func (s *counterWatchStore) SaveICDCounter(ctx context.Context, v uint32) error {
	if s.watch.Load() {
		s.saves.Add(1)
	}
	return s.Store.SaveICDCounter(ctx, v)
}

// TestICDManagementNoCheckInPassAfterStop (R2-C2-conc-1005): a wake that
// decided before Stop and reaches the Check-In start after it — the mode
// machine calls OnActiveEntered after releasing its lock — starts no pass:
// nothing is sent and the counter is not written to the store after Stop
// returned.
func TestICDManagementNoCheckInPassAfterStop(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := &counterWatchStore{Store: memstore.New()}
		if _, err := st.AddFabric(t.Context(), store.FabricRecord{
			FabricID: 1, RootPublicKey: bytes.Repeat([]byte{1}, 65), RootCert: []byte{1}, VendorID: 0xFFF1,
		}); err != nil {
			t.Fatal(err)
		}
		s, err := NewICDManagement(ICDManagementConfig{
			IdleModeDuration: 4, MaximumCheckInBackoff: 4, ActiveModeDuration: 2000, ActiveModeThreshold: 1000,
			ClientsSupportedPerFabric: 2, Store: st, Clock: clocktest.New(time.Unix(1_700_000_000, 0)),
		})
		if err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		sent := 0
		s.SetCheckInSender(func(context.Context, icd.CheckInRequest) (bool, error) {
			mu.Lock()
			sent++
			mu.Unlock()
			return true, nil
		})
		if err := s.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // the start's own pass
		s.Stop()

		// A Permanent client a pass would serve, and a stored counter far
		// enough behind that the pass would write the counter first.
		s.mu.Lock()
		s.fabricLocked(1).Set(icd.Registration{CheckInNodeID: 0x10, MonitoredSubject: 0x10, Key: bytes.Repeat([]byte{1}, 16), ClientType: icd.ClientPermanent})
		s.counterSaved = s.counter.Value() - icd.BootBump
		s.mu.Unlock()
		st.watch.Store(true)

		s.activeEntered() // the wake decided before Stop
		synctest.Wait()
		if n := st.saves.Load(); n != 0 {
			t.Errorf("the counter was written %d times after Stop", n)
		}
		mu.Lock()
		defer mu.Unlock()
		if sent != 0 {
			t.Errorf("%d Check-Ins sent after Stop", sent)
		}
	})
}
