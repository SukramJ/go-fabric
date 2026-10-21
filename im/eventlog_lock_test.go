// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"sync"
	"testing"
	"time"
)

// TestEventLog_PersistRunsWithoutTheLock pins G1-16 (ADR 0026 P6): the
// counter ceiling is persisted — the host's store — with the log's lock
// released, so a persist that reads the log does not deadlock; and under
// concurrent Appends no number is handed out before a ceiling covering it
// was persisted.
func TestEventLog_PersistRunsWithoutTheLock(t *testing.T) {
	t.Parallel()
	log := NewEventLog()
	var (
		mu     sync.Mutex
		stored uint64
	)
	log.SetCounterPersistence(func(ceiling uint64) {
		_ = log.Query(0, 0, 0, 0) // reads the log from inside the store write
		mu.Lock()
		if ceiling > stored {
			stored = ceiling
		}
		mu.Unlock()
	}, 4)

	done := make(chan struct{})
	var bad sync.Map
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				for range 50 {
					n := log.Append(EventRecord{Priority: EventPriorityInfo})
					mu.Lock()
					covered := n < stored
					mu.Unlock()
					if !covered {
						bad.Store(n, true)
					}
				}
			})
		}
		wg.Wait()
		log.DropBuffered()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("the event log holds its lock while it persists the ceiling")
	}
	bad.Range(func(k, _ any) bool {
		t.Errorf("number %v handed out before a ceiling covering it was stored", k)
		return true
	})
}
