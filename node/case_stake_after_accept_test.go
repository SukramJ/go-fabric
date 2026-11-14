// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// TestAnUnacceptedSigma1ClosesNoLiveSession: with every session id held by
// a live session, a Sigma1 that does not start a handshake — it does not
// even parse — closes none of them: the CASE responder stakes its session
// id once a Sigma1 is accepted, not when its adapter is built (review II,
// R2-P3-dr-dev-08).
func TestAnUnacceptedSigma1ClosesNoLiveSession(t *testing.T) {
	t.Parallel()
	sessions := operational.NewManager(testStore(t))
	for i := range operational.SessionIDSpace {
		id, err := sessions.AllocateID()
		if err != nil {
			t.Fatalf("AllocateID %d: %v", i, err)
		}
		// Sessions this node opened, which the per-peer bound leaves
		// alone, so the table fills without a walk per session.
		if _, err := sessions.OpenFromSigmaAsInitiatorWithID(id, 1, 0x1, uint64(0x1000+i), 0x100, nil, sigma.SessionKeys{}); err != nil {
			t.Fatalf("OpenFromSigmaAsInitiatorWithID %d: %v", i, err)
		}
	}
	if occ := sessions.Occupancy(); occ.Live != operational.SessionIDSpace {
		t.Fatalf("occupancy %+v, want every id held by a live session", occ)
	}
	ids := newCaseIdentities(slog.New(slog.DiscardHandler), nil)
	provider := bridge.NewPerExchangeCaseProvider(func() *bridge.CaseAdapter {
		adapter, err := buildCaseAdapter(sessions, ids, nil, slog.New(slog.DiscardHandler), nil)
		if err != nil {
			return nil
		}
		return adapter
	})
	h := provider.Resolve(bridge.HandshakeKey{InitiatorNodeID: 0xBAD0BAD0, ExchangeID: 1})
	if h == nil {
		t.Fatal("no handler for the Sigma1")
	}
	_, _, _ = h.ProcessSigma1([]byte{0x15, 0x18})
	if occ := sessions.Occupancy(); occ.Live != operational.SessionIDSpace {
		t.Fatalf("after a Sigma1 that does not parse: occupancy %+v, want every live session kept", occ)
	}
}
