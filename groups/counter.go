// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// GroupDataCounterKey is the matter_metadata key of the node-global Group
// Encrypted Data Message Counter. Mirrors matter.js
// packages/protocol/src/session/SessionManager.ts:192 at 0ceb202d
// (GROUP_DATA_COUNTER_KEY).
const GroupDataCounterKey = "groupDataCounter"

const (
	// groupDataCounterReserve is how far ahead of the counter the
	// persisted value is kept, CHIP GROUP_MSG_COUNTER_MIN_INCREMENT.
	// Mirrors SessionManager.ts:199 (GROUP_DATA_COUNTER_RESERVE).
	groupDataCounterReserve = 1000
	// maxCounterValue is the largest 32-bit counter value; the counter
	// wraps from it to 0. Mirrors MessageCounter.ts:10
	// (MAX_COUNTER_VALUE_32BIT).
	maxCounterValue = 0xFFFF_FFFF
	// rolloverInfoDifference is how many values before the wrap the
	// near-rollover warning fires. Mirrors MessageCounter.ts:13
	// (ROLLOVER_INFO_DIFFERENCE).
	rolloverInfoDifference = 100_000
)

// CounterStore persists the group data counter. *store.Store satisfies it
// (GetMetadataCounter / SetMetadataCounter on matter_metadata); a [Store]
// that does not implement it cannot send group messages
// ([ErrNoCounterStore]).
type CounterStore interface {
	GetMetadataCounter(ctx context.Context, key string) (value uint64, ok bool, err error)
	SetMetadataCounter(ctx context.Context, key string, value uint64) error
}

// ErrNoCounterStore is returned by [Manager.Sender] when the manager's
// store keeps no counter ([CounterStore]): matter.js persists the group
// data counter unconditionally (§4.6.1.3, "Persistence: Mandatory",
// MessageCounter.ts:22-26), so sending without one is refused.
var ErrNoCounterStore = errors.New("groups: the store cannot persist the group data counter")

// dataCounter is the node-global group data message counter: matter.js
// PersistedMessageCounter (packages/protocol/src/protocol/MessageCounter.ts:
// 100-177) as SessionManager.#createGroupDataMessageCounter
// (SessionManager.ts:981-1007) configures it — persisted under
// [GroupDataCounterKey] with a reserve of 1000, rolling over to 0 with a
// warning near the end instead of failing.
//
// The persisted value is a high mark: before a value at or past it is
// handed out, a new mark 1000 ahead is stored, so a restart — clean or not
// — resumes above every value already sent. The same reserve-before-use
// pattern keeps event numbers monotonic in this module
// (im/eventlog.go:173, "Persist a fresh ceiling BEFORE handing the number
// out").
type dataCounter struct {
	st     CounterStore
	logger *slog.Logger

	mu       sync.Mutex
	value    uint64 // the last value handed out (MessageCounter.messageCounter)
	reserved uint64 // the persisted high mark (#reserved)
}

// randomCounterStart is the random start of a counter without a persisted
// value: a 28-bit random number plus 1. Mirrors MessageCounter.ts:57-60
// (`(crypto.randomUint32 >>> 4) + 1`).
func randomCounterStart() (uint32, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("groups: counter seed: %w", err)
	}
	return binary.LittleEndian.Uint32(b[:])>>4 + 1, nil
}

// newDataCounter restores the counter from st, or starts it at random on
// the first run, and persists the first reserve. random is the seam for
// the random start. Mirrors PersistedMessageCounter's construction
// (MessageCounter.ts:118-154) with SessionManager.ts:984-1000:
//
//   - :131-143 a persisted value is the counter (a value beyond 32 bits is
//     an error, :133-135); at or within 100 000 of the wrap the
//     near-rollover warning fires once (:137-143);
//   - :144-149 without one the seed applies — matter.js seeds from the
//     legacy per-key send counters (SessionManager.ts:984-985,
//     MessagingState.ts:41-60); this module never sent a group message and
//     has none, so the seed is absent and the random start of
//     MessageCounter.ts:60 stays;
//   - :150-152 then #reserveAhead stores the first mark.
func newDataCounter(ctx context.Context, st CounterStore, logger *slog.Logger, random func() (uint32, error)) (*dataCounter, error) {
	c := &dataCounter{st: st, logger: logger}
	stored, ok, err := st.GetMetadataCounter(ctx, GroupDataCounterKey)
	if err != nil {
		return nil, fmt.Errorf("groups: load group data counter: %w", err)
	}
	if ok {
		if stored > maxCounterValue {
			return nil, fmt.Errorf("groups: invalid group data counter value %d", stored)
		}
		c.value = stored
		if c.value >= maxCounterValue-rolloverInfoDifference {
			c.aboutToRollover()
		}
	} else {
		start, err := random()
		if err != nil {
			return nil, err
		}
		c.value = uint64(start)
	}
	if err := c.reserveAhead(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// reserveAhead persists the mark reserve values past the counter, capped
// at the wrap. Mirrors MessageCounter.ts:161-164 (#reserveAhead). Called
// with c.mu held, or before c is shared.
func (c *dataCounter) reserveAhead(ctx context.Context) error {
	reserved := min(c.value+groupDataCounterReserve, maxCounterValue)
	if err := c.st.SetMetadataCounter(ctx, GroupDataCounterKey, reserved); err != nil {
		return fmt.Errorf("groups: persist group data counter: %w", err)
	}
	c.reserved = reserved
	return nil
}

// next advances the counter and returns the new value. Mirrors
// PersistedMessageCounter.getIncrementedCounter (MessageCounter.ts:166-176)
// over MessageCounter.getIncrementedCounter (:63-78):
//
//   - :64-70 the counter increments and, past 0xFFFFFFFF, wraps to 0 — the
//     group data counter has a rollover callback, so it never throws;
//   - :71-76 at exactly 100 000 before the wrap the warning fires
//     (SessionManager.ts:995-999, spec §4.6.4: the group epoch keys should
//     be rotated);
//   - :170-174 at or past the mark, or below the mark's window after the
//     wrap, a new mark is persisted before the value is handed out.
func (c *dataCounter) next(ctx context.Context) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
	if c.value > maxCounterValue {
		c.value = 0
	} else if c.value == maxCounterValue-rolloverInfoDifference {
		c.aboutToRollover()
	}
	if c.value >= c.reserved || c.value+groupDataCounterReserve < c.reserved {
		if err := c.reserveAhead(ctx); err != nil {
			return 0, err
		}
	}
	return uint32(c.value), nil //nolint:gosec // wrapped to 0 above past maxCounterValue
}

// aboutToRollover is the aboutToRolloverCallback of
// SessionManager.ts:995-999: the node cannot rotate the group epoch keys
// itself (the Administrator does, §4.17.3.3), so it only warns.
func (c *dataCounter) aboutToRollover() {
	c.logger.Warn("matter.groups.counter_near_rollover",
		slog.Uint64("counter", c.value),
		slog.String("hint", "group epoch keys should be rotated to avoid message counter reuse"))
}

// groupDataCounter returns the node's group data counter, creating it on
// first use. A failed creation is not cached, so the next send retries.
func (m *Manager) groupDataCounter(ctx context.Context) (*dataCounter, error) {
	m.counterMu.Lock()
	defer m.counterMu.Unlock()
	if m.counter != nil {
		return m.counter, nil
	}
	cs, ok := m.st.(CounterStore)
	if !ok {
		return nil, ErrNoCounterStore
	}
	c, err := newDataCounter(ctx, cs, m.logger, m.counterRandom)
	if err != nil {
		return nil, err
	}
	m.counter = c
	return c, nil
}
