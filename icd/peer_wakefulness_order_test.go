// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd_test

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestPeerWakefulnessDeliversInChangeOrder holds R2-P6-conc-0208: when a
// timer's expiry (available → false) and a NoteActive (→ true) change the
// state concurrently, the observers get the two in the order the changes
// happened, so the value an observer saw last is the state's. The
// observer of the expiry is held inside its callback while NoteActive
// runs on another goroutine; before the fix NoteActive delivered at once
// and the held false landed last.
func TestPeerWakefulnessDeliversInChangeOrder(t *testing.T) {
	t.Parallel()
	w, fake, _ := litPeer()
	w.NoteActive()
	if !w.Available() {
		t.Fatal("not available after activity")
	}
	var (
		mu       sync.Mutex
		last     bool
		seen     []bool
		held     = make(chan struct{})
		release  = make(chan struct{})
		holdOnce sync.Once
	)
	w.OnAvailableChanged(func(v bool) {
		if !v {
			holdOnce.Do(func() {
				close(held)
				<-release
			})
		}
		mu.Lock()
		last = v
		seen = append(seen, v)
		mu.Unlock()
	})
	advanced := make(chan struct{})
	go func() {
		defer close(advanced)
		fake.Advance(checkInDue + time.Millisecond)
	}()
	<-held
	noted := make(chan struct{})
	go func() {
		defer close(noted)
		w.NoteActive()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-noted:
	case <-ctx.Done():
		t.Fatal("NoteActive blocked behind an observer's callback")
	}
	close(release)
	<-advanced
	mu.Lock()
	defer mu.Unlock()
	if last != w.Available() {
		t.Fatalf("observer saw %v last (all: %v), state is available=%v", last, seen, w.Available())
	}
}
