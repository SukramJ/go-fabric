// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"fmt"
	"math"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// SessionIntervals are the MRP intervals the node reports in the PASE and
// CASE session parameters and advertises in its operational DNS-SD record
// (SII, SAI, SAT): how long a peer waits before it retransmits to this
// node. A zero field is the Matter default. Mirrors matter.js
// SessionsBehavior.State.intervals (SessionsBehavior.ts:115-120 at
// 862ad75f), which ServerNetworkRuntime.ts:357-363 hands the
// SessionManager's sessionParameters and DeviceAdvertiser.ts:361-364 the
// operational advertisement; omitted values fall back to
// SessionIntervals.defaults (SessionIntervals.ts:36-50).
type SessionIntervals struct {
	// Idle is SESSION_IDLE_INTERVAL; 0 is 500 ms.
	Idle time.Duration
	// Active is SESSION_ACTIVE_INTERVAL; 0 is 300 ms.
	Active time.Duration
	// ActiveThreshold is SESSION_ACTIVE_THRESHOLD; 0 is 4000 ms. At most
	// 65535 ms, the uint16 the session parameters carry.
	ActiveThreshold time.Duration
}

// The Matter defaults (SessionIntervals.ts:45-50) and the bound of the
// active threshold (:63, maxActiveThreshold = Millis(65535)).
const (
	defaultSessionIdle            = 500 * time.Millisecond
	defaultSessionActive          = 300 * time.Millisecond
	defaultSessionActiveThreshold = 4000 * time.Millisecond
	maxSessionActiveThreshold     = 65535 * time.Millisecond
	maxSessionInterval            = time.Duration(math.MaxUint32) * time.Millisecond
)

// validate returns one error per field the session parameters cannot
// carry. matter.js refuses a local active threshold above 65535 ms
// (SessionManager.ts:58-67, assertActiveThreshold, called by the
// sessionParameters setter :368-377) and leaves the uint32 idle and active
// intervals unbounded; here a negative value, a value off whole
// milliseconds and an interval above the uint32 range are refused too,
// because the wire carries whole milliseconds.
func (s *SessionIntervals) validate() []error {
	if s == nil {
		return nil
	}
	var errs []error
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
			errs = append(errs, fmt.Errorf("node: Sessions.%s %v is not whole milliseconds from 0 to %v", f.name, f.v, f.upper))
		}
	}
	return errs
}

// resolved is s with each zero field at its default (SessionIntervals.ts:
// 36-42); a nil s is the defaults.
func (s *SessionIntervals) resolved() SessionIntervals {
	r := SessionIntervals{Idle: defaultSessionIdle, Active: defaultSessionActive, ActiveThreshold: defaultSessionActiveThreshold}
	if s == nil {
		return r
	}
	if s.Idle != 0 {
		r.Idle = s.Idle
	}
	if s.Active != 0 {
		r.Active = s.Active
	}
	if s.ActiveThreshold != 0 {
		r.ActiveThreshold = s.ActiveThreshold
	}
	return r
}

// advertised is the operational record's share of s.
func (s *SessionIntervals) advertised() bridge.SessionIntervals {
	r := s.resolved()
	return bridge.SessionIntervals{Idle: r.Idle, Active: r.Active, ActiveThreshold: r.ActiveThreshold}
}

// apply writes s into p, the responders' session parameters.
func (s *SessionIntervals) apply(p *sessionparams.Parameters) {
	r := s.resolved()
	p.SessionIdleInterval = uint32(r.Idle / time.Millisecond)               //nolint:gosec // validate bounds it to uint32
	p.SessionActiveInterval = uint32(r.Active / time.Millisecond)           //nolint:gosec // validate bounds it to uint32
	p.SessionActiveThreshold = uint16(r.ActiveThreshold / time.Millisecond) //nolint:gosec // validate bounds it to uint16
}

// SessionParameters are the session parameters the node reports to every
// PASE and CASE peer (PBKDFParamResponse tag 5, Sigma2's
// responderSessionParams): the intervals of [Config.Sessions], the node's
// MaxPathsPerInvoke and its TCP roles. Mirrors matter.js
// SessionManager.sessionParameters (SessionManager.ts:351-360).
func (n *Node) SessionParameters() sessionparams.Parameters {
	return *n.sec.responderParams
}
