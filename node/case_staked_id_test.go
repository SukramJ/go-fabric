// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// TestCASEHandshakesThatDoNotFinishLeaveNoSessionIDReserved: the production
// CASE factory stakes a session id for each adapter it builds
// (buildCaseAdapter); a handshake that fails, and an adapter the provider
// drops unfinished, give the id back at once instead of leaving it reserved
// for [operational.PlaceholderIDTTL] (review II, R2-P3-lan-dev-02).
func TestCASEHandshakesThatDoNotFinishLeaveNoSessionIDReserved(t *testing.T) {
	t.Parallel()
	sessions := operational.NewManager(testStore(t))
	ids := newCaseIdentities(slog.New(slog.DiscardHandler), nil)
	provider := bridge.NewPerExchangeCaseProvider(func() *bridge.CaseAdapter {
		adapter, err := buildCaseAdapter(sessions, ids, nil, slog.New(slog.DiscardHandler), nil)
		if err != nil {
			return nil
		}
		return adapter
	})

	const handshakes = 64
	for i := range handshakes {
		// A malformed Sigma1 fails its handshake.
		key := bridge.HandshakeKey{InitiatorNodeID: 0xBAD0BAD0, ExchangeID: uint16(i + 1)}
		if h := provider.Resolve(key); h == nil {
			t.Fatalf("handshake %d: no handler", i)
		} else {
			_, _, _ = h.ProcessSigma1([]byte{0x15, 0x18})
		}
		// An adapter built and never used is dropped by its provider.
		provider.Resolve(bridge.HandshakeKey{InitiatorNodeID: 0xC0DE, ExchangeID: uint16(i + 1)})
	}
	provider.Reset()

	if occ := sessions.Occupancy(); occ.Reserved != 0 || occ.Live != 0 {
		t.Fatalf("after %d failed and %d dropped handshakes: occupancy %+v, want nothing reserved", handshakes, handshakes, occ)
	}
}
