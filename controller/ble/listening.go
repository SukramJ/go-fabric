// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

import (
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
)

// ListeningMeter accumulates the time a [Central]'s radio scanned, which
// the central reports as [ListeningClock.ListeningTime] — matter.js
// BleListeningClock (packages/protocol/src/common/BleListeningClock.ts:9-37).
// A central calls Start when its radio reports that it scans and Stop when
// the radio stopped or can no longer scan. It is safe for concurrent use;
// the zero value is not usable, use [NewListeningMeter].
type ListeningMeter struct {
	clock exchange.Clock

	// mu guards running, since and total.
	mu      sync.Mutex
	since   time.Time
	running bool
	total   time.Duration
}

// NewListeningMeter returns a meter on clk; nil is the system clock.
func NewListeningMeter(clk exchange.Clock) *ListeningMeter {
	if clk == nil {
		clk = exchange.SystemClock{}
	}
	return &ListeningMeter{clock: clk}
}

// Start records that the radio scans; a repeated Start while it scans
// keeps the first start (BleListeningClock.ts:16-19).
func (m *ListeningMeter) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		m.since, m.running = m.clock.Now(), true
	}
}

// Stop records that the radio stopped scanning; a Stop without a running
// scan changes nothing (BleListeningClock.ts:21-28).
func (m *ListeningMeter) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	m.total += m.clock.Now().Sub(m.since)
	m.running = false
}

// Total is the scan time so far, a scan still running included
// (BleListeningClock.ts:30-36).
func (m *ListeningMeter) Total() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return m.total
	}
	return m.total + m.clock.Now().Sub(m.since)
}
