// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/sessions/SessionsBehaviorTest.ts
// at 862ad75f, "retains defaults for intervals left unset".
//
// matter.js configures the node's session intervals (`sessions.intervals`)
// and reads the SessionManager's sessionParameters; node.Config carries no
// interval, so every interval is left unset, and the observable of the
// node's session parameters is what its CASE responder states in Sigma2:
// the controller's peer of the commissioned device reports them
// (Peer.SessionParameters, Peer.ts:281-317). "applies configured intervals
// …" needs the node option, "reports session activity on the wall clock"
// MockTime.stepWallClock, which clocktest.Fake has not (the register names
// both).
package node_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
)

// SessionIntervals.defaults (SessionIntervals.ts:45-49): idle 500 ms,
// active 300 ms, active threshold 4 s.
const (
	defaultActiveIntervalMs  = 300
	defaultActiveThresholdMs = 4000
)

// TestSessionsBehaviorParityMatterJS ports the describe "intervals
// configuration" (SessionsBehaviorTest.ts:14-38).
func TestSessionsBehaviorParityMatterJS(t *testing.T) {
	holdSite(t)

	// :27-37.
	t.Run("retains defaults for intervals left unset", func(t *testing.T) {
		p := quietSite(t).CommissionedPair(fabrictest.PairOptions{})
		got := p.Peer.SessionParameters()
		if got.SessionActiveInterval != defaultActiveIntervalMs {
			t.Errorf("activeInterval %d ms, want the default %d ms", got.SessionActiveInterval, defaultActiveIntervalMs)
		}
		if got.SessionActiveThreshold != defaultActiveThresholdMs {
			t.Errorf("activeThreshold %d ms, want the default %d ms", got.SessionActiveThreshold, defaultActiveThresholdMs)
		}
	})
}
