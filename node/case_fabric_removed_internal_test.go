// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// rawKeyVerifier treats a NOC as the bare uncompressed public key of its
// holder and knows each fixture's subject, so a CASE handshake runs
// without certificates.
type rawKeyVerifier struct {
	mu       *sync.Mutex
	subjects map[string][2]uint64 // noc → {node id, fabric id}
}

func (v rawKeyVerifier) VerifyAndExtractPubKey(noc, _ []byte) (*ecdsa.PublicKey, error) {
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), noc)
}

func (v rawKeyVerifier) subject(noc []byte) ([2]uint64, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	s, ok := v.subjects[string(noc)]
	if !ok {
		return s, errors.New("rawKeyVerifier: unknown noc")
	}
	return s, nil
}

func (v rawKeyVerifier) PeerNodeIDFromNOC(noc []byte) (uint64, error) {
	s, err := v.subject(noc)
	return s[0], err
}

func (v rawKeyVerifier) PeerFabricIDFromNOC(noc []byte) (uint64, error) {
	s, err := v.subject(noc)
	return s[1], err
}

// rawKeyIdentity returns an identity on fabricID whose NOC is its raw
// public key, registered with v.
func rawKeyIdentity(t *testing.T, v rawKeyVerifier, nodeID, fabricID uint64, fabricIndex uint8, ipk [16]byte) *sigma.Identity {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := priv.PublicKey.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	noc := pub.Bytes()
	v.mu.Lock()
	v.subjects[string(noc)] = [2]uint64{nodeID, fabricID}
	v.mu.Unlock()
	return &sigma.Identity{NOC: noc, PrivateKey: priv, NodeID: nodeID, FabricID: fabricID, FabricIndex: fabricIndex, IPK: ipk}
}

// caseFabricFixture is fabric 1 loaded into a caseIdentities table, with
// an identity for a peer of the same fabric.
type caseFabricFixture struct {
	ids      *caseIdentities
	verifier rawKeyVerifier
	root     []byte
	peer     *sigma.Identity
	local    *sigma.Identity
}

func newCaseFabricFixture(t *testing.T) caseFabricFixture {
	t.Helper()
	v := rawKeyVerifier{mu: &sync.Mutex{}, subjects: map[string][2]uint64{}}
	var ipk [16]byte
	if _, err := rand.Read(ipk[:]); err != nil {
		t.Fatal(err)
	}
	rootKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := rootKey.PublicKey().Bytes()
	local := rawKeyIdentity(t, v, 0xB0B, 0xFAB, 1, ipk)
	peer := rawKeyIdentity(t, v, 0xC0FFEE, 0xFAB, 1, ipk)
	ids := newCaseIdentities(slog.New(slog.DiscardHandler), nil)
	f := &caseFabric{identity: local, verifier: v, rootPublicKey: root, ipks: [][16]byte{ipk}}
	ids.byIdx[1], ids.latest = f, f
	return caseFabricFixture{ids: ids, verifier: v, root: root, peer: peer, local: local}
}

// TestCASEResponderRefusesASessionOfARemovedFabric: a CASE handshake
// that is between Sigma2 and Sigma3 when its fabric is removed does not
// open a session on the removed fabric's index once Sigma3 arrives — the
// removal swept the fabric's sessions before it existed. Ahead of
// matter.js, whose CaseServer creates the session with the fabric it
// resolved at Sigma1 (review II, R2-P2-sess-05, R2-P2-fabric-09).
func TestCASEResponderRefusesASessionOfARemovedFabric(t *testing.T) {
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
	_, sigma2, err := adapter.ProcessSigma1(sigma1)
	if err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}
	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatalf("ProcessSigma2: %v", err)
	}

	// RemoveFabric of fabric 1 (node.fabricRemoved): the identity is
	// forgotten, then the fabric's sessions are closed.
	if _, _, ok := fx.ids.forget(1); !ok {
		t.Fatal("forget(1) found no identity")
	}
	sessions.CloseFabric(1)

	if _, _, err := adapter.ProcessSigma3(sigma3); err == nil {
		t.Fatal("Sigma3 of a handshake whose fabric was removed completed")
	}
	for _, s := range sessions.Sessions() {
		if s.FabricIndex == 1 {
			t.Fatalf("a session on the removed fabric's index is open: %+v", s)
		}
	}
}

// TestCASEInitiatorRefusesASessionOfARemovedFabric: the same for a
// handshake this node opened (the re-establishment of a former
// subscription): its completion opens nothing once the fabric is gone.
// Review II, R2-P2-fabric-09.
func TestCASEInitiatorRefusesASessionOfARemovedFabric(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	sessions := operational.NewManager(testStore(t))
	provider := caseInitiatorProvider(testStore(t), sessions, fx.ids, slog.New(slog.DiscardHandler), nil)
	init, err := provider(context.Background(), 1, fx.peer.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := fx.ids.forget(1); !ok {
		t.Fatal("forget(1) found no identity")
	}
	sessions.CloseFabric(1)

	var keys sigma.SessionKeys
	_, _ = rand.Read(keys.I2RKey[:])
	_, _ = rand.Read(keys.R2IKey[:])
	err = init.OnEstablished(context.Background(), sigma.InitiatorResult{PeerNodeID: fx.peer.NodeID, PeerSessionID: 0x2002, Keys: keys})
	if err == nil {
		t.Fatal("a handshake whose fabric was removed opened its session")
	}
	for _, s := range sessions.Sessions() {
		if s.FabricIndex == 1 {
			t.Fatalf("a session on the removed fabric's index is open: %+v", s)
		}
	}
}

// TestAdmitSessionClosesASessionWhoseFabricWentWhileItOpened: a removal
// that forgets the fabric's identity while the session is being opened —
// after the first check, so its own sweep may have run before the session
// existed — leaves the second check without the identity, and the session
// is closed again (review II, R2-P2-sess-05).
func TestAdmitSessionClosesASessionWhoseFabricWentWhileItOpened(t *testing.T) {
	t.Parallel()
	fx := newCaseFabricFixture(t)
	sessions := operational.NewManager(testStore(t))
	var keys sigma.SessionKeys
	_, _ = rand.Read(keys.I2RKey[:])
	_, _ = rand.Read(keys.R2IKey[:])
	var opened uint16
	_, err := fx.ids.admitSession(sessions, 1, func() (*operational.Entry, error) {
		fx.ids.forget(1)
		sessions.CloseFabric(1) // finds nothing yet
		e, err := sessions.OpenFromSigma(1, fx.local.NodeID, fx.peer.NodeID, keys)
		if e != nil {
			opened = e.SessionID
		}
		return e, err
	})
	if !errors.Is(err, errCASEFabricRemoved) {
		t.Fatalf("admitSession = %v, want errCASEFabricRemoved", err)
	}
	if opened == 0 {
		t.Fatal("the open did not run")
	}
	if _, err := sessions.Get(opened); err == nil {
		t.Fatalf("session %d of the removed fabric is still open", opened)
	}
}
