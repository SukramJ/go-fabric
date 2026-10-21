// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/CommissioningPaseLifetimeTest.ts
// at 862ad75f: when commissioning cannot continue past PASE, the
// commissioner closes the PASE session it established.
//
// matter.js injects a commissioning-flow class whose constructor throws
// (commissioningFlowImpl). The module's commissioner builds its flow without
// such a seam; the step that can refuse to continue between PASE and the
// flow is ClaimNodeIDAfterPASE (controller/commissioner Options), which the
// port makes refuse. matter.js then asserts that its SessionManager holds no
// PASE session; the port asserts that the controller's exchange manager
// holds no session the attempt opened (the manager's sessions by local id,
// before and after).
package node_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// openSessions are the secure sessions of mgr, by local id.
func openSessions(mgr *exchange.Manager) map[uint16]*exchange.Session {
	out := map[uint16]*exchange.Session{}
	for id := range 1 << 16 {
		if s := mgr.Session(uint16(id)); s != nil && !s.IsClosed() {
			out[uint16(id)] = s
		}
	}
	return out
}

func TestCommissioningPaseLifetimeParity(t *testing.T) {
	t.Parallel()
	t.Run("closes the PASE session when the commissioning flow cannot be created", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		u := site.UncommissionedPair(fabrictest.PairOptions{})
		before := openSessions(u.Controller.Exchanges())
		claimed := false
		var during map[uint16]*exchange.Session
		_, err := u.Controller.Commission(parityCtx(t), u.Device, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
			o.ClaimNodeIDAfterPASE = func() (uint64, bool) {
				claimed, during = true, openSessions(u.Controller.Exchanges())
				return 0, false
			}
		}})
		if err == nil || !errors.Is(err, commissioner.ErrCommissioning) {
			t.Fatalf("commissioning: %v, want a commissioning error", err)
		}
		if !claimed || len(during) <= len(before) {
			t.Fatalf("no PASE session open at the refusal (%d sessions, %d before)", len(during), len(before))
		}
		for id, s := range openSessions(u.Controller.Exchanges()) {
			if before[id] == nil {
				t.Fatalf("session %d (peer node %d) left open after the refusal", id, s.PeerNodeID())
			}
		}
	})
}
