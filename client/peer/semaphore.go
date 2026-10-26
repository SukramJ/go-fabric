// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// ErrSemaphoreClosed is a slot request of a closed or cleared
// [Semaphore] — matter.js's ClosedError("Queue is closed") and ("Queue
// cleared") (packages/general/src/util/Semaphore.ts:73, :227, :252 at
// 862ad75f).
var ErrSemaphoreClosed = errors.New("peer: semaphore closed")

// Semaphore limits concurrent work by slots: a caller obtains a [Slot],
// works, and closes it. A delay is a cooldown between consecutive grants;
// a timeout releases a slot its holder forgot. It is matter.js's
// Semaphore (packages/general/src/util/Semaphore.ts:38-256 at 862ad75f),
// the throttle of a network profile. It is safe for concurrent use.
type Semaphore struct {
	clock       clock.Clock
	scope       string
	concurrency int
	delay       time.Duration
	timeout     time.Duration

	mu      sync.Mutex
	queue   []*slotWaiter
	running int
	granted uint64
	// cooling is the delay timer: a grant starts it,
	// and while it runs no further grant is made (Semaphore.ts:55). coolingGen tells a
	// stopped timer's late callback from the current one's.
	cooling    clock.Timer
	coolingGen uint64
	closed     bool
}

// slotWaiter is one queued request: grant receives its slot, failed is
// closed when Clear or Close drops it.
type slotWaiter struct {
	grant  chan *Slot
	failed chan struct{}
}

// Slot is a grant of a [Semaphore]; Close releases it. It is idempotent.
type Slot struct {
	s       *Semaphore
	once    sync.Once
	timeout clock.Timer
}

// NewSemaphore returns a semaphore of concurrency slots (below 1 is 1, as
// matter.js's default concurrency; [UnlimitedExchanges] for no limit), delay
// between grants (0 for none) and timeout auto-releasing a slot (0 for
// none), on clk (nil is the system clock) — Semaphore.ts:51-57.
func NewSemaphore(clk clock.Clock, scope string, concurrency int, delay, timeout time.Duration) *Semaphore {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Semaphore{clock: clock.Or(clk), scope: scope, concurrency: concurrency, delay: delay, timeout: timeout}
}

// Obtain waits for a slot: at once when one is free, nobody waits and no
// cooldown runs, else in arrival order. ctx ends the wait (its error);
// a closed semaphore is [ErrSemaphoreClosed] (Semaphore.ts:70-126).
func (s *Semaphore) Obtain(ctx context.Context) (*Slot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrSemaphoreClosed
	}
	if s.running < s.concurrency && len(s.queue) == 0 && (s.delay == 0 || s.cooling == nil) {
		slot := s.grantLocked()
		s.mu.Unlock()
		return slot, nil
	}
	w := &slotWaiter{grant: make(chan *Slot, 1), failed: make(chan struct{})}
	s.queue = append(s.queue, w)
	s.scheduleLocked()
	s.mu.Unlock()
	select {
	case slot := <-w.grant:
		return slot, nil
	case <-w.failed:
		return nil, ErrSemaphoreClosed
	case <-ctx.Done():
		if !s.withdraw(w) {
			// Granted (or dropped) while we gave up: hand a grant back.
			select {
			case slot := <-w.grant:
				slot.Close()
			default:
			}
		}
		return nil, ctx.Err()
	}
}

// withdraw drops w from the queue; false when it was granted already.
func (s *Semaphore) withdraw(w *slotWaiter) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, q := range s.queue {
		if q == w {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			return true
		}
	}
	return false
}

// scheduleLocked grants the next waiter now, or starts the cooldown that
// will (Semaphore.ts:131-144).
func (s *Semaphore) scheduleLocked() {
	if s.cooling != nil || len(s.queue) == 0 || s.running >= s.concurrency {
		return
	}
	if s.delay == 0 {
		s.processNextLocked()
		return
	}
	s.startCoolingLocked()
}

func (s *Semaphore) startCoolingLocked() {
	s.coolingGen++
	gen := s.coolingGen
	s.cooling = s.clock.AfterFunc(s.delay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.cooling == nil || s.coolingGen != gen {
			return
		}
		s.cooling = nil
		s.processNextLocked()
	})
}

func (s *Semaphore) stopCoolingLocked() {
	if s.cooling != nil {
		s.cooling.Stop()
		s.cooling = nil
	}
}

// processNextLocked grants the next waiter a slot (Semaphore.ts:201-219).
func (s *Semaphore) processNextLocked() {
	if len(s.queue) == 0 || s.running >= s.concurrency {
		return
	}
	w := s.queue[0]
	s.queue = s.queue[1:]
	w.grant <- s.grantLocked()
	s.scheduleLocked()
}

// grantLocked counts a running slot, starts the cooldown and the slot's
// timeout (Semaphore.ts:149-188).
func (s *Semaphore) grantLocked() *Slot {
	s.running++
	s.granted++
	if s.delay > 0 {
		s.stopCoolingLocked()
		s.startCoolingLocked()
	}
	slot := &Slot{s: s}
	if s.timeout > 0 {
		slot.timeout = s.clock.AfterFunc(s.timeout, slot.Close)
	}
	return slot
}

// Close releases the slot: the next waiter may be granted
// (Semaphore.ts:160-168, :193-196).
func (sl *Slot) Close() {
	if sl == nil {
		return
	}
	sl.once.Do(func() {
		if sl.timeout != nil {
			sl.timeout.Stop()
		}
		s := sl.s
		s.mu.Lock()
		defer s.mu.Unlock()
		s.running--
		s.scheduleLocked()
	})
}

// Running is the number of granted slots (Semaphore.ts:243-245).
func (s *Semaphore) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Granted is the number of slots granted since the semaphore was made —
// a diagnostic of which profile an interaction went through (matter.js
// tests record NetworkProfiles.select for it, ClientNodeInteractionIcdTest.ts:78-106).
func (s *Semaphore) Granted() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.granted
}

// Count is the number of waiting requests (Semaphore.ts:236-238).
func (s *Semaphore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// Clear fails every waiting request with [ErrSemaphoreClosed]; the
// semaphore stays usable (Semaphore.ts:224-231).
func (s *Semaphore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearLocked()
}

func (s *Semaphore) clearLocked() {
	for _, w := range s.queue {
		close(w.failed)
	}
	s.queue = nil
}

// Close fails every waiting and every later request (Semaphore.ts:250-255).
func (s *Semaphore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.clearLocked()
	s.stopCoolingLocked()
}
