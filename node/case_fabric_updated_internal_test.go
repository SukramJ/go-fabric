// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// updateIdentity stands in for an UpdateNOC of fabric 1 (node.fabricUpdated):
// the identity is loaded again — the next load — and the fabric's
// sessions are swept.
func (fx caseFabricFixture) updateIdentity(sessions *operational.Manager) {
	fx.ids.mu.Lock()
	next := *fx.ids.byIdx[1]
	fx.ids.loads++
	next.gen = fx.ids.loads
	fx.ids.byIdx[1], fx.ids.latest = &next, &next
	fx.ids.mu.Unlock()
	sessions.CloseFabric(1)
}

// TestCASEResponderRefusesASessionAcrossAnUpdate (review II,
// R2-P6-sec-2412): a CASE handshake between Sigma2 and Sigma3 when an
// UpdateNOC replaces its fabric's identity opens no session once Sigma3
// arrives — the update's sweep ran before it existed — as connectedhomeip
// aborts a handshake pending on an updated fabric.
func TestCASEResponderRefusesASessionAcrossAnUpdate(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	st := fabricStore(t, fx) // holds fabric 1: a record of it could be stored
	sessions := operational.NewManager(st)
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
	_, sigma2, err := adapter.ProcessSigma1(sigma1)
	if err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}
	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatalf("ProcessSigma2: %v", err)
	}
	fx.updateIdentity(sessions)
	if _, _, err := adapter.ProcessSigma3(sigma3); err == nil {
		t.Fatal("Sigma3 of a handshake whose fabric was updated completed")
	}
	for _, s := range sessions.Sessions() {
		if s.FabricIndex == 1 {
			t.Fatalf("a session made under the replaced identity is open: %+v", s)
		}
	}
	// Sessions lists no responder session still establishing: the table
	// itself holds no session either.
	if live := sessions.Occupancy().Live; live != 0 {
		t.Fatalf("%d sessions with keys in the table, want none", live)
	}
	if rec, err := st.GetResumptionByPeer(context.Background(), 1, fx.peer.NodeID); err == nil {
		t.Fatalf("a resumption record made under the replaced identity was stored: %+v", rec)
	}
}

// TestCASEInitiatorRefusesASessionAcrossAnUpdate (review II,
// R2-P6-sec-2412): the same for a handshake this node opened.
func TestCASEInitiatorRefusesASessionAcrossAnUpdate(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	st := fabricStore(t, fx) // holds fabric 1: a record of it could be stored
	sessions := operational.NewManager(st)
	provider := caseInitiatorProvider(st, sessions, fx.ids, slog.New(slog.DiscardHandler), nil)
	init, err := provider(context.Background(), 1, fx.peer.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	fx.updateIdentity(sessions)
	var keys sigma.SessionKeys
	_, _ = rand.Read(keys.I2RKey[:])
	_, _ = rand.Read(keys.R2IKey[:])
	err = init.OnEstablished(context.Background(), sigma.InitiatorResult{PeerNodeID: fx.peer.NodeID, PeerSessionID: 0x2002, Keys: keys, ResumptionID: make([]byte, 16), SharedSecret: make([]byte, 32)})
	if !errors.Is(err, errCASEFabricUpdated) {
		t.Fatalf("OnEstablished across an update: %v, want errCASEFabricUpdated", err)
	}
	for _, s := range sessions.Sessions() {
		if s.FabricIndex == 1 {
			t.Fatalf("a session made under the replaced identity is open: %+v", s)
		}
	}
	// Sessions lists no responder session still establishing: the table
	// itself holds no session either.
	if live := sessions.Occupancy().Live; live != 0 {
		t.Fatalf("%d sessions with keys in the table, want none", live)
	}
	if rec, err := st.GetResumptionByPeer(context.Background(), 1, fx.peer.NodeID); err == nil {
		t.Fatalf("a resumption record made under the replaced identity was stored: %+v", rec)
	}
}

// TestCASEHandshakeWithinOneIdentityIsAdmitted: a handshake whose
// fabric's identity is not replaced meanwhile admits its session.
func TestCASEHandshakeWithinOneIdentityIsAdmitted(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	st := testStore(t)
	sessions := operational.NewManager(st)
	provider := caseInitiatorProvider(st, sessions, fx.ids, slog.New(slog.DiscardHandler), nil)
	init, err := provider(context.Background(), 1, fx.peer.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	var keys sigma.SessionKeys
	_, _ = rand.Read(keys.I2RKey[:])
	_, _ = rand.Read(keys.R2IKey[:])
	if err := init.OnEstablished(context.Background(), sigma.InitiatorResult{PeerNodeID: fx.peer.NodeID, PeerSessionID: 0x2002, Keys: keys, ResumptionID: make([]byte, 16), SharedSecret: make([]byte, 32)}); err != nil {
		t.Fatalf("OnEstablished: %v", err)
	}
}

// TestCASEResponderRefusesASessionWhoseUpdateFollowsItsStake (review II,
// R2-P6-sec-2412): an UpdateNOC that lands after the responder took its
// stake — the identity loaded again and the fabric's sessions swept before
// the session is installed — refuses the install: the admission compares
// the load the handshake answered under, not only that the fabric exists.
func TestCASEResponderRefusesASessionWhoseUpdateFollowsItsStake(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	st := testStore(t)
	sessions := operational.NewManager(st)
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
	_, sigma2, err := adapter.ProcessSigma1(sigma1)
	if err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}
	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatalf("ProcessSigma2: %v", err)
	}
	fx.ids.testAfterStake = func() { fx.updateIdentity(sessions) }
	if _, _, err := adapter.ProcessSigma3(sigma3); err == nil {
		t.Fatal("Sigma3 completed although an update landed after its stake")
	}
	for _, s := range sessions.Sessions() {
		if s.FabricIndex == 1 {
			t.Fatalf("a session made under the replaced identity is open: %+v", s)
		}
	}
	// Sessions lists no responder session still establishing: the table
	// itself holds no session either.
	if live := sessions.Occupancy().Live; live != 0 {
		t.Fatalf("%d sessions with keys in the table, want none", live)
	}
}

// TestCASEResponderKeepsTheResolutionOfItsAcceptedSigma1 (review II,
// decision 244): a Sigma1 on the exchange of a handshake in progress that
// resolves its destination and is then refused — here an invalid
// ephemeral key, after an UpdateNOC of the fabric — leaves the handshake
// answering under the load it resolved for its own Sigma1. Its Sigma3 is
// refused as across any update; it is not admitted under the newer load
// the refused Sigma1 resolved.
func TestCASEResponderKeepsTheResolutionOfItsAcceptedSigma1(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	st := testStore(t)
	sessions := operational.NewManager(st)
	adapter, err := buildCaseAdapter(sessions, fx.ids, nil, slog.New(slog.DiscardHandler), nil)
	if err != nil {
		t.Fatal(err)
	}
	newInitiator := func(id uint16) *sigma.Initiator {
		t.Helper()
		in, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
			Identity: fx.peer, Verifier: fx.verifier, SessionID: id,
			PeerNodeID: fx.local.NodeID, RootPublicKey: fx.root,
		})
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	initiator := newInitiator(0x1001)
	sigma1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	_, sigma2, err := adapter.ProcessSigma1(sigma1)
	if err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}
	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatalf("ProcessSigma2: %v", err)
	}
	fx.updateIdentity(sessions)

	// A second Sigma1 whose destination resolves under the new load, with
	// its ephemeral key's point prefix spoiled: refused after the resolve.
	bad, err := newInitiator(0x1002).GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(bad, []byte{0x30, 0x04, 0x41, 0x04})
	if at < 0 {
		t.Fatal("no ephemeral key in the Sigma1")
	}
	bad[at+3] = 0x05
	if op, _, err := adapter.ProcessSigma1(bad); err != nil || op != mrp.SCOpcodeStatusReport {
		t.Fatalf("the spoiled Sigma1: opcode %#x, err %v; want a StatusReport", op, err)
	}

	if _, _, err := adapter.ProcessSigma3(sigma3); err == nil {
		t.Fatal("Sigma3 completed under the load a refused Sigma1 resolved")
	}
	for _, s := range sessions.Sessions() {
		if s.FabricIndex == 1 {
			t.Fatalf("a session made under the replaced identity is open: %+v", s)
		}
	}
	// Sessions lists no responder session still establishing: the table
	// itself holds no session either.
	if live := sessions.Occupancy().Live; live != 0 {
		t.Fatalf("%d sessions with keys in the table, want none", live)
	}
}
