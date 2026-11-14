// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
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

// TestAFailedCASEHandshakeReleasesOnlyItsOwnStake: at a full id space an
// allocation takes the oldest staked id for a new handshake
// (BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly); the handshake that
// had staked it, dropped unfinished afterwards, gives back its own stake
// and nothing else — the id stays staked for the handshake that holds it
// now (review II, R2-P3-dr-dev-07).
func TestAFailedCASEHandshakeReleasesOnlyItsOwnStake(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	sessions := operational.NewManager(testStore(t))
	provider := bridge.NewPerExchangeCaseProvider(func() *bridge.CaseAdapter {
		adapter, err := buildCaseAdapter(sessions, fx.ids, nil, slog.New(slog.DiscardHandler), nil)
		if err != nil {
			return nil
		}
		return adapter
	})
	// begin answers a Sigma1 addressed to the fixture's fabric on key and
	// returns the session id the handshake announced.
	begin := func(key bridge.HandshakeKey) uint16 {
		t.Helper()
		initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
			Identity: fx.peer, Verifier: fx.verifier, SessionID: 0x1001,
			PeerNodeID: fx.local.NodeID, RootPublicKey: fx.root,
		})
		if err != nil {
			t.Fatal(err)
		}
		sigma1, err := initiator.GenerateSigma1()
		if err != nil {
			t.Fatal(err)
		}
		h, ok := provider.Resolve(key).(*bridge.CaseAdapter)
		if !ok {
			t.Fatal("no adapter")
		}
		if op, _, err := h.ProcessSigma1(sigma1); err != nil || op != mrp.SCOpcodeSigma2 {
			t.Fatalf("Sigma1 answered with opcode %#x, %v; want Sigma2", op, err)
		}
		return h.SnapshotResponder().SessionID()
	}
	first := bridge.HandshakeKey{InitiatorNodeID: 0xC0FFEE, ExchangeID: 1}
	staked := begin(first)
	// Every other id staked too: the table is full of stakes.
	for range operational.SessionIDSpace - 1 {
		if _, err := sessions.AllocateID(); err != nil {
			t.Fatalf("AllocateID: %v", err)
		}
	}
	if occ := sessions.Occupancy(); occ.Reserved != operational.SessionIDSpace {
		t.Fatalf("occupancy %+v, want every id staked", occ)
	}
	if got := begin(bridge.HandshakeKey{InitiatorNodeID: 0xC0FFEE, ExchangeID: 2}); got != staked {
		t.Fatalf("the second handshake staked id %d, want the oldest staked id %d", got, staked)
	}
	provider.Forget(first) // the first handshake is dropped unfinished

	if next, err := sessions.AllocateID(); err != nil || next == staked {
		t.Fatalf("after the first handshake let go, AllocateID = (%d, %v): the id the second handshake holds was free again", next, err)
	}
}
