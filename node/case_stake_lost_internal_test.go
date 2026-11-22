// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"testing"

	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// fillSessionIDsAndTakeOldest stakes every free id of sessions, then
// allocates once more: the allocation at the full table takes the oldest
// stake's id, which it returns with its stake.
func fillSessionIDsAndTakeOldest(t *testing.T, sessions *operational.Manager) operational.Stake {
	t.Helper()
	occ := sessions.Occupancy()
	for range operational.SessionIDSpace - occ.Reserved - occ.Live {
		if _, err := sessions.AllocateStake(); err != nil {
			t.Fatalf("AllocateStake: %v", err)
		}
	}
	taker, err := sessions.AllocateStake()
	if err != nil {
		t.Fatalf("AllocateStake at a full table: %v", err)
	}
	return taker
}

// TestCASEResponderWhoseStakeWasTakenOverOpensNoSession (review II,
// N3-findev-1): a responder handshake whose staked id an allocation at a
// full table gave to another handshake completes its Sigma3 without a
// session, and the id stays staked for the handshake that holds it now.
func TestCASEResponderWhoseStakeWasTakenOverOpensNoSession(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	sessions := operational.NewManager(testStore(t))
	adapter, err := buildCaseAdapter(sessions, fx.ids, nil, slog.New(slog.DiscardHandler), nil)
	if err != nil {
		t.Fatal(err)
	}
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
	op, sigma2, err := adapter.ProcessSigma1(sigma1)
	if err != nil || op != mrp.SCOpcodeSigma2 {
		t.Fatalf("Sigma1 answered with opcode %#x, %v; want Sigma2", op, err)
	}
	staked := adapter.SnapshotResponder().SessionID()
	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatalf("ProcessSigma2: %v", err)
	}

	taker := fillSessionIDsAndTakeOldest(t, sessions)
	if taker.ID != staked {
		t.Fatalf("the allocation at the full table took id %d, want the handshake's %d", taker.ID, staked)
	}

	if _, _, err := adapter.ProcessSigma3(sigma3); err == nil {
		t.Fatal("Sigma3 of a handshake whose stake was taken over opened a session")
	}
	if occ := sessions.Occupancy(); occ.Live != 0 || occ.Reserved != operational.SessionIDSpace {
		t.Fatalf("occupancy %+v, want no session and every id still staked", occ)
	}
	sessions.ReleaseStake(taker)
	if occ := sessions.Occupancy(); occ.Reserved != operational.SessionIDSpace-1 {
		t.Fatalf("after the taker released its stake: occupancy %+v, want its id free — the refused install replaced the stake", occ)
	}
}

// TestCASEInitiatorWhoseStakeWasTakenOverOpensNoSession: the same for a
// handshake this node opened (the re-establishment of a former
// subscription): its completion opens nothing under an id another
// handshake holds now, and answers operational.ErrStakeLost.
func TestCASEInitiatorWhoseStakeWasTakenOverOpensNoSession(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	sessions := operational.NewManager(testStore(t))
	provider := caseInitiatorProvider(testStore(t), sessions, fx.ids, slog.New(slog.DiscardHandler), nil)
	init, err := provider(context.Background(), 1, fx.peer.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	taker := fillSessionIDsAndTakeOldest(t, sessions)

	var keys sigma.SessionKeys
	_, _ = rand.Read(keys.I2RKey[:])
	_, _ = rand.Read(keys.R2IKey[:])
	err = init.OnEstablished(context.Background(), sigma.InitiatorResult{PeerNodeID: fx.peer.NodeID, PeerSessionID: 0x2002, Keys: keys})
	if !errors.Is(err, operational.ErrStakeLost) {
		t.Fatalf("OnEstablished of a handshake whose stake was taken over = %v, want operational.ErrStakeLost", err)
	}
	if occ := sessions.Occupancy(); occ.Live != 0 || occ.Reserved != operational.SessionIDSpace {
		t.Fatalf("occupancy %+v, want no session and every id still staked", occ)
	}
	init.OnAbandoned()
	sessions.ReleaseStake(taker)
	if occ := sessions.Occupancy(); occ.Reserved != operational.SessionIDSpace-1 {
		t.Fatalf("after the taker released its stake: occupancy %+v, want its id free", occ)
	}
}

// TestAdmitSessionClosesOnlyTheSessionItOpened (review II, decision 159,
// Fable Q1 record): when the fabric went while the session opened,
// admitSession closes the session it installed, not whatever holds that
// session's id by then. The stand-in for the race: inside the open, the
// installed session is closed (as an eviction would) and its id — the only
// free one — is staked by another handshake before admitSession closes.
func TestAdmitSessionClosesOnlyTheSessionItOpened(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	sessions := operational.NewManager(testStore(t))
	var own operational.Stake
	for range operational.SessionIDSpace {
		s, err := sessions.AllocateStake()
		if err != nil {
			t.Fatalf("AllocateStake: %v", err)
		}
		own = s
	}
	var keys sigma.SessionKeys
	_, _ = rand.Read(keys.I2RKey[:])
	_, _ = rand.Read(keys.R2IKey[:])
	var other operational.Stake
	_, err := fx.ids.admitSession(sessions, 1, func() (*operational.Entry, error) {
		e, err := sessions.OpenFromSigmaWithStake(own, 1, fx.local.NodeID, fx.peer.NodeID, 0x2002, nil, keys)
		if err != nil {
			return nil, err
		}
		fx.ids.forget(1)
		if err := sessions.Close(e.SessionID); err != nil {
			t.Fatalf("close the installed session: %v", err)
		}
		if other, err = sessions.AllocateStake(); err != nil || other.ID != e.SessionID {
			t.Fatalf("AllocateStake = (%d, %v), want the only free id %d", other.ID, err, e.SessionID)
		}
		return e, nil
	})
	if !errors.Is(err, errCASEFabricRemoved) {
		t.Fatalf("admitSession = %v, want errCASEFabricRemoved", err)
	}
	if occ := sessions.Occupancy(); occ.Reserved != operational.SessionIDSpace {
		t.Fatalf("occupancy %+v, want every id staked: admitSession's close took the other handshake's stake", occ)
	}
}
