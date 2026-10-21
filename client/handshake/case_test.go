// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

const (
	testFabricID uint64 = 0x0000_0000_0000_0F0B
	testNodeID   uint64 = 0x0000_0000_0000_1234
)

// commissionedFabric starts a node, commissions it with the test
// commissioner and returns the node's address and our side of the fabric
// as controller node 112233, the identity the commissioner's ACL entry
// names.
func commissionedFabric(t *testing.T) (*net.UDPAddr, handshake.Fabric) {
	t.Helper()
	n, addr := startNode(t)
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), bridgetest.Options{
		Passcode: testPasscode, Discriminator: 3840, FabricID: testFabricID, NodeID: testNodeID, Seed: 7,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := c.Commission(ctx)
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	s.Close()
	id := c.ControllerIdentity(bridgetest.DefaultControllerNodeID)
	id.FabricIndex = 1
	verifier, err := mattercert.NewVerifier(c.RootPublicKey(), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	return addr, handshake.Fabric{Identity: &id, RootPublicKey: c.RootPublicKey(), Verifier: verifier}
}

// TestCASEAndResumption: a full CASE handshake with a commissioned node,
// then a second one that resumes from the record the first saved; both
// sessions carry IM traffic.
func TestCASEAndResumption(t *testing.T) {
	t.Parallel()
	addr, fabric := commissionedFabric(t)
	m, rec := newManager(t)
	store := &handshake.MemoryResumptionStore{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The exchange options reach the Sigma exchange (CASEOptions.Exchange,
	// OnExchange): the connection process's notifiers.
	var sends, receipts atomic.Int32
	var opened atomic.Pointer[exchange.Exchange]
	s, resumed, err := handshake.CASE(ctx, m, addr, fabric, testNodeID, handshake.CASEOptions{
		Resumption: store, CaseAuthenticatedTags: []uint32{},
		Exchange: exchange.Options{
			OnSend:    func(int) { sends.Add(1) },
			OnReceive: func() { receipts.Add(1) },
		},
		OnExchange: opened.Store,
	})
	if err != nil {
		t.Fatalf("CASE: %v", err)
	}
	if opened.Load() == nil || sends.Load() < 2 || receipts.Load() < 2 {
		t.Fatalf("exchange options: opened %v, %d sends, %d receipts", opened.Load() != nil, sends.Load(), receipts.Load())
	}
	if resumed || s.Resumed() || s.PeerNodeID() != testNodeID || s.NodeID() != bridgetest.DefaultControllerNodeID {
		t.Fatalf("session resumed=%v peer 0x%X local %d", resumed, s.PeerNodeID(), s.NodeID())
	}
	if r := s.ReportedParameters(); r == nil || r.MaxPathsPerInvoke == 0 {
		t.Fatalf("Sigma2 session parameters not reported: %+v", r)
	}
	readNodeLabel(t, m, s)
	record, ok := store.FindResumptionRecord(handshake.PeerAddress{FabricIndex: 1, NodeID: testNodeID})
	if !ok || len(record.ResumptionID) != 16 || len(record.SharedSecret) == 0 || record.SessionParameters.SessionIdleInterval == 0 {
		t.Fatalf("resumption record %+v (%v)", record, ok)
	}

	s2, resumed, err := handshake.CASE(ctx, m, addr, fabric, testNodeID, handshake.CASEOptions{Resumption: store})
	if err != nil {
		t.Fatalf("second CASE: %v", err)
	}
	if !resumed || !s2.Resumed() || s2.ID() == s.ID() {
		t.Fatalf("second CASE resumed=%v id %d (first %d)", resumed, s2.ID(), s.ID())
	}
	readNodeLabel(t, m, s2)
	again, _ := store.FindResumptionRecord(handshake.PeerAddress{FabricIndex: 1, NodeID: testNodeID})
	if bytes.Equal(again.ResumptionID, record.ResumptionID) || !bytes.Equal(again.SharedSecret, record.SharedSecret) {
		t.Fatal("the resumed handshake did not move the record to the new resumption id")
	}
	if n := invalidParams(rec.unsecuredStatusReports()); n != 0 {
		t.Fatalf("%d InvalidParam reports on success", n)
	}
	m.CloseSession(ctx, s)
	m.CloseSession(ctx, s2)
}

// TestCASEStaleRecordFallsBackToFullHandshake: a record the node does not
// know makes it answer Sigma2; the new record replaces the stale one and
// the session takes the record's CATs when the options name none
// (CaseClient.ts:333-334, :357-364).
func TestCASEStaleRecordFallsBackToFullHandshake(t *testing.T) {
	t.Parallel()
	addr, fabric := commissionedFabric(t)
	m, _ := newManager(t)
	store := &handshake.MemoryResumptionStore{}
	stale := handshake.ResumptionRecord{
		Peer:                  handshake.PeerAddress{FabricIndex: 1, NodeID: testNodeID},
		SharedSecret:          make([]byte, 32),
		ResumptionID:          make([]byte, 16),
		CaseAuthenticatedTags: []uint32{0x0001_0001},
	}
	if err := store.SaveResumptionRecord(stale); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, resumed, err := handshake.CASE(ctx, m, addr, fabric, testNodeID, handshake.CASEOptions{Resumption: store})
	if err != nil {
		t.Fatalf("CASE: %v", err)
	}
	if resumed {
		t.Fatal("resumed from a record the node never issued")
	}
	if cats := s.CaseAuthenticatedTags(); len(cats) != 1 || cats[0] != 0x0001_0001 {
		t.Fatalf("session CATs %v, want the record's", cats)
	}
	rec, _ := store.FindResumptionRecord(stale.Peer)
	if bytes.Equal(rec.ResumptionID, stale.ResumptionID) {
		t.Fatal("the stale record was not replaced")
	}
	readNodeLabel(t, m, s)
}

// TestCASEUnknownDestination: Sigma1 for a node id the node is not makes
// it answer with a StatusReport error, which is transient — the peer
// reported the failure itself, so no InvalidParam goes back
// (CaseClient.ts:86-89).
func TestCASEUnknownDestination(t *testing.T) {
	t.Parallel()
	addr, fabric := commissionedFabric(t)
	m, rec := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, err := handshake.CASE(ctx, m, addr, fabric, testNodeID+1, handshake.CASEOptions{})
	var se *exchange.StatusReportError
	if !errors.As(err, &se) || !errors.Is(err, exchange.ErrTransient) {
		t.Fatalf("CASE to an unknown node = %v, want a StatusReportError", err)
	}
	if n := invalidParams(rec.unsecuredStatusReports()); n != 0 {
		t.Fatalf("%d InvalidParam reports sent, want none", n)
	}
}

// TestCASEPeerChainDoesNotVerify: a responder NOC that does not chain to
// the root we verify against fails the handshake after Sigma2
// (CaseClient.ts:309, fabric.verifyCredentials); that is data the peer
// sent, so InvalidParam goes back (CaseClient.ts:83-99).
func TestCASEPeerChainDoesNotVerify(t *testing.T) {
	t.Parallel()
	addr, fabric := commissionedFabric(t)
	m, rec := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	other := bridgetest.Dial(t, addr.String(), bridgetest.Options{Passcode: testPasscode, FabricID: testFabricID, NodeID: testNodeID, Seed: 99})
	wrongRoot, err := mattercert.NewVerifier(other.RootPublicKey(), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	fabric.Verifier = wrongRoot
	_, _, err = handshake.CASE(ctx, m, addr, fabric, testNodeID, handshake.CASEOptions{})
	if !errors.Is(err, sigma.ErrSignatureInvalid) || errors.Is(err, exchange.ErrTransient) {
		t.Fatalf("CASE with a verifier of another root = %v, want sigma.ErrSignatureInvalid", err)
	}
	if n := invalidParams(rec.unsecuredStatusReports()); n != 1 {
		t.Fatalf("%d InvalidParam reports sent, want 1", n)
	}
	if _, _, err := handshake.CASE(ctx, m, addr, handshake.Fabric{}, testNodeID, handshake.CASEOptions{}); err == nil {
		t.Fatal("CASE without an identity accepted")
	}
}
