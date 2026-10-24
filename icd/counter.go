// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// BootBump is the counter advance applied on every boot: a counter value,
// and with it its AES-CCM nonce, is reused after a crash only if more than
// this many increments were lost unpersisted (Core §4.6.3). Mirrors
// matter.js IcdCounter.ts:16 (BOOT_BUMP).
const BootBump = 1000

// ErrCounterAdvance reports an advance outside 0 to 0xffffffff, matter.js's
// ImplementationError of IcdCounter.ts:78-80.
var ErrCounterAdvance = errors.New("icd: an ICD counter advances by an integer from 0 to 0xffffffff")

// Counter is the device's Check-In counter, the ICDCounter attribute
// (quality "N C"). Its owner constructs it from the persisted value,
// persists [Counter.Value] once, then persists every value its change
// callback reports (Core §9.16.6.5, §4.6.3). Values wrap at 2^32: a client
// measures offsets modulo 2^32 and refreshes its key before 2^31.
//
// Mirrors matter.js protocol/src/icd/IcdCounter.ts:32-85 (IcdCounter).
type Counter struct {
	// mu guards value and changed.
	mu      sync.Mutex
	value   uint32
	changed func(uint32)
}

// RandomInitialValue is the counter of a factory-reset device: a 28-bit
// random value plus one, 1 to 2^28 (Core §4.6.1.1, §4.6.3). Mirrors
// matter.js IcdCounter.ts:46-48 (randomInitialValue).
func RandomInitialValue() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails (Go 1.24+)
	return binary.LittleEndian.Uint32(b[:])>>4 + 1
}

// NewCounter returns the counter of persistedValue advanced by [BootBump],
// wrapping at 2^32 (IcdCounter.ts:50-52).
func NewCounter(persistedValue uint32) *Counter {
	return &Counter{value: persistedValue + BootBump}
}

// OnChange sets the callback that receives the new value after every
// [Counter.Increment] and [Counter.Advance] — the owner's persistence
// (IcdCounter.ts:36, changed). It runs on the caller's goroutine, after
// the counter's lock is released; nil removes it. Two advances racing
// each other may report out of order, so the owner advances from one
// goroutine at a time (the ICD Management server's Check-In pass and
// its command queue).
func (c *Counter) OnChange(fn func(uint32)) {
	c.mu.Lock()
	c.changed = fn
	c.mu.Unlock()
}

// Value is the current counter value (IcdCounter.ts:55-57).
func (c *Counter) Value() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// Increment advances the counter by one and returns the new value
// (IcdCounter.ts:64-66).
func (c *Counter) Increment() uint32 {
	v, _ := c.Advance(1) // 1 is always in range
	return v
}

// Advance advances the counter by `by`, wrapping at 2^32, reports the new
// value to the change callback and returns it. It is for the test event
// triggers that invalidate counter values, as CHIP's
// InvalidateHalfCheckInCounterValues does; an advance outside 0 to
// 0xffffffff returns [ErrCounterAdvance] and changes nothing.
//
// Mirrors matter.js IcdCounter.ts:77-84 (advance).
func (c *Counter) Advance(by int64) (uint32, error) {
	if by < 0 || by > 0xffffffff {
		return 0, fmt.Errorf("%w, not %d", ErrCounterAdvance, by)
	}
	c.mu.Lock()
	c.value += uint32(by)
	v, fn := c.value, c.changed
	c.mu.Unlock()
	if fn != nil {
		fn(v)
	}
	return v, nil
}
