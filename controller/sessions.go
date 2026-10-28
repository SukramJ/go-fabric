// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// SessionIntervals are the controller's own session intervals, reported
// to every peer in its handshakes ([Config].Sessions). The same shape as
// node.SessionIntervals; matter.js keeps one SessionIntervals for both
// roles (SessionIntervals.ts:45-50 at 862ad75f, the defaults; the active
// threshold is bounded by the uint16 it is encoded as, SessionManager.ts:58-67).
type SessionIntervals struct {
	// Idle is SESSION_IDLE_INTERVAL; 0 is 500 ms.
	Idle time.Duration
	// Active is SESSION_ACTIVE_INTERVAL; 0 is 300 ms.
	Active time.Duration
	// ActiveThreshold is SESSION_ACTIVE_THRESHOLD; 0 is 4000 ms. At most
	// 65535 ms.
	ActiveThreshold time.Duration
}

const (
	maxSessionInterval        = time.Duration(^uint32(0)) * time.Millisecond
	maxSessionActiveThreshold = 65535 * time.Millisecond
)

// validate reports the fields outside whole milliseconds and their bounds.
func (s *SessionIntervals) validate() error {
	if s == nil {
		return nil
	}
	for _, f := range []struct {
		name  string
		v     time.Duration
		upper time.Duration
	}{
		{"Idle", s.Idle, maxSessionInterval},
		{"Active", s.Active, maxSessionInterval},
		{"ActiveThreshold", s.ActiveThreshold, maxSessionActiveThreshold},
	} {
		if f.v < 0 || f.v > f.upper || f.v%time.Millisecond != 0 {
			return fmt.Errorf("%w: Sessions.%s %v is not whole milliseconds from 0 to %v", ErrConfiguration, f.name, f.v, f.upper)
		}
	}
	return nil
}

// apply writes the set intervals over p; a zero field keeps p's value.
func (s *SessionIntervals) apply(p *sessionparams.Parameters) {
	if s == nil {
		return
	}
	if s.Idle > 0 {
		p.SessionIdleInterval = uint32(s.Idle / time.Millisecond) //nolint:gosec // bounded by validate
	}
	if s.Active > 0 {
		p.SessionActiveInterval = uint32(s.Active / time.Millisecond) //nolint:gosec // bounded by validate
	}
	if s.ActiveThreshold > 0 {
		p.SessionActiveThreshold = uint16(s.ActiveThreshold / time.Millisecond) //nolint:gosec // bounded by validate
	}
}
