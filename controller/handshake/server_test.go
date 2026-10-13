// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// controllerNodeID is our node id on the responder tests' fabric.
const controllerNodeID uint64 = 0x0000_0000_0001_1111

// responderRig is a CASE server on one manager (the controller) and a
// second manager acting as the node that opens CASE to it, both holding
// identities a bridgetest CA minted on one fabric.
type responderRig struct {
	server     *handshake.CASEServer
	serverMgr  *exchange.Manager
	serverRec  *recorder
	serverAddr *net.UDPAddr
	store      *handshake.MemoryResumptionStore
	sessions   chan *exchange.Session

	nodeMgr    *exchange.Manager
	nodeFabric handshake.Fabric
	nodeStore  *handshake.MemoryResumptionStore
	ca         *bridgetest.Commissioner
}

// fabricOf is our side of the bridgetest CA's fabric as node nodeID with
// fabric index index.
func fabricOf(t *testing.T, c *bridgetest.Commissioner, nodeID uint64, index uint8) handshake.Fabric {
	t.Helper()
	id := c.ControllerIdentity(nodeID)
	id.FabricIndex = index
	verifier, err := mattercert.NewVerifier(c.RootPublicKey(), mattercert.SystemTime{})
	if err != nil {
		t.Fatal(err)
	}
	return handshake.Fabric{Identity: &id, RootPublicKey: c.RootPublicKey(), Verifier: verifier}
}

func newResponderRig(t *testing.T) *responderRig {
	t.Helper()
	c := bridgetest.Dial(t, "127.0.0.1:5540", bridgetest.Options{Passcode: testPasscode, FabricID: testFabricID, NodeID: testNodeID, Seed: 11})
	r := &responderRig{
		store:     &handshake.MemoryResumptionStore{},
		nodeStore: &handshake.MemoryResumptionStore{},
		sessions:  make(chan *exchange.Session, 8),
		ca:        c,
	}
	r.serverMgr, r.serverRec = newManager(t)
	r.serverAddr = r.serverRec.l.LocalAddr()
	server, err := handshake.NewCASEServer(r.serverMgr, handshake.CASEServerConfig{
		Fabrics:    handshake.FabricList{fabricOf(t, c, controllerNodeID, 1)},
		Resumption: r.store,
		OnSession: func(s *exchange.Session, fabricIndex uint8) {
			if fabricIndex != 1 || s.FabricIndex() != 1 {
				t.Errorf("OnSession fabric index %d / %d", fabricIndex, s.FabricIndex())
			}
			r.sessions <- s
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.Register()
	t.Cleanup(server.Close)
	r.server = server
	r.nodeMgr, _ = newManager(t)
	// The node's own fabric index for the fabric need not be ours.
	r.nodeFabric = fabricOf(t, c, testNodeID, 3)
	return r
}

// open runs the node's CASE to the controller.
func (r *responderRig) open(ctx context.Context, fabric handshake.Fabric) (*exchange.Session, bool, error) {
	return handshake.CASE(ctx, r.nodeMgr, r.serverAddr, fabric, controllerNodeID, handshake.CASEOptions{Resumption: r.nodeStore})
}

func (r *responderRig) accepted(t *testing.T) *exchange.Session {
	t.Helper()
	select {
	case s := <-r.sessions:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("no session reached OnSession")
		return nil
	}
}

// ping sends one reliable IM message on s and waits for the
// acknowledgement — proof that both directions' keys hold.
func ping(t *testing.T, m *exchange.Manager, s *exchange.Session) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	x, err := m.Initiate(s, im.InteractionModelProtocolID, exchange.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	if err := x.Send(ctx, im.OpcodeStatusResponse, []byte{0x15, 0x24, 0x00, 0x00, 0x18}, exchange.SendOptions{}); err != nil {
		t.Fatalf("ping on session %d: %v", s.ID(), err)
	}
}

func statusCount(reports []exchange.StatusReport, general, code uint16) int {
	n := 0
	for _, r := range reports {
		if r.GeneralCode == general && r.ProtocolCode == code {
			n++
		}
	}
	return n
}

// TestCASEServerFullAndResumed: the node opens CASE to the controller —
// a full handshake (Sigma1, Sigma2, Sigma3, Success; CaseServer.ts:224-347),
// then a second one resumed from the record both sides saved
// (Sigma2_Resume, the initiator's Success; :148-222). Both sessions reach
// OnSession, carry traffic in both directions, and the record moves to the
// resumption id Sigma2_Resume issued.
func TestCASEServerFullAndResumed(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, resumed, err := r.open(ctx, r.nodeFabric)
	if err != nil {
		t.Fatalf("CASE to the controller: %v", err)
	}
	server := r.accepted(t)
	if resumed || server.Resumed() || server.PeerNodeID() != testNodeID || server.NodeID() != controllerNodeID || server.FabricIndex() != 1 {
		t.Fatalf("server session resumed %v peer %X local %X fabric %d", server.Resumed(), server.PeerNodeID(), server.NodeID(), server.FabricIndex())
	}
	if server.PeerSessionID() != client.ID() || client.PeerSessionID() != server.ID() {
		t.Fatalf("session ids: server %d→%d, client %d→%d", server.ID(), server.PeerSessionID(), client.ID(), client.PeerSessionID())
	}
	if p := server.ReportedParameters(); p == nil || p.SessionIdleInterval == 0 {
		t.Fatalf("Sigma1 session parameters not reported: %+v", p)
	}
	ping(t, r.nodeMgr, client)
	ping(t, r.serverMgr, server)
	rec, ok := r.store.FindResumptionRecord(handshake.PeerAddress{FabricIndex: 1, NodeID: testNodeID})
	nodeRec, _ := r.nodeStore.FindResumptionRecord(handshake.PeerAddress{FabricIndex: 3, NodeID: controllerNodeID})
	if !ok || !bytes.Equal(rec.ResumptionID, nodeRec.ResumptionID) || !bytes.Equal(rec.SharedSecret, nodeRec.SharedSecret) {
		t.Fatalf("server record %+v (%v) does not match the node's %+v", rec, ok, nodeRec)
	}
	if byID, ok := r.store.FindByResumptionID(rec.ResumptionID); !ok || byID.Peer != rec.Peer {
		t.Fatalf("FindByResumptionID = %+v, %v", byID, ok)
	}

	client2, resumed, err := r.open(ctx, r.nodeFabric)
	if err != nil {
		t.Fatalf("second CASE: %v", err)
	}
	server2 := r.accepted(t)
	if !resumed || !server2.Resumed() || server2.ID() == server.ID() || server2.PeerNodeID() != testNodeID {
		t.Fatalf("second handshake resumed %v/%v, ids %d/%d", resumed, server2.Resumed(), server2.ID(), server.ID())
	}
	ping(t, r.nodeMgr, client2)
	ping(t, r.serverMgr, server2)
	moved, _ := r.store.FindResumptionRecord(rec.Peer)
	nodeMoved, _ := r.nodeStore.FindResumptionRecord(nodeRec.Peer)
	if bytes.Equal(moved.ResumptionID, rec.ResumptionID) || !bytes.Equal(moved.ResumptionID, nodeMoved.ResumptionID) {
		t.Fatal("the resumed handshake did not move both records to the new resumption id")
	}
	if n := len(r.serverRec.unsecuredStatusReports()); n != 1 {
		t.Fatalf("%d StatusReports sent, want the one Success of the full handshake", n)
	}
}

// TestCASEServerUnknownFabric: a Sigma1 whose destination id names no
// fabric of ours is answered with NoSharedTrustRoots (FabricManager.ts:333,
// CaseServer.ts:88-90); the node sees the peer's status, a transient error.
func TestCASEServerUnknownFabric(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	other := bridgetest.Dial(t, "127.0.0.1:5540", bridgetest.Options{Passcode: testPasscode, FabricID: testFabricID + 1, NodeID: testNodeID, Seed: 12})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, err := r.open(ctx, fabricOf(t, other, testNodeID, 1))
	var se *exchange.StatusReportError
	if !errors.As(err, &se) || se.Report.ProtocolCode != mrp.SCStatusProtocolNoSharedTrustRoots {
		t.Fatalf("CASE to a foreign fabric = %v, want NoSharedTrustRoots", err)
	}
	if n := statusCount(r.serverRec.unsecuredStatusReports(), mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolNoSharedTrustRoots); n != 1 {
		t.Fatalf("%d NoSharedTrustRoots reports sent, want 1", n)
	}
}

// TestCASEServerBadSigma3: a Sigma3 whose signature does not verify ends
// the handshake with InvalidParam (CaseServer.ts:309, :93); no session.
func TestCASEServerBadSigma3(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	bad := r.nodeFabric
	id := *bad.Identity
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id.PrivateKey = key
	bad.Identity = &id
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, err = r.open(ctx, bad)
	var se *exchange.StatusReportError
	if !errors.As(err, &se) || se.Report.ProtocolCode != mrp.SCStatusProtocolInvalidParameter {
		t.Fatalf("CASE with a forged Sigma3 = %v, want InvalidParam", err)
	}
	if n := invalidParams(r.serverRec.unsecuredStatusReports()); n != 1 {
		t.Fatalf("%d InvalidParam reports sent, want 1", n)
	}
	select {
	case s := <-r.sessions:
		t.Fatalf("a session was established: %d", s.ID())
	default:
	}
}

// TestCASEServerResumeMICFallsBack: a resuming Sigma1 whose MIC does not
// decrypt with the record's secret falls back to the full handshake
// silently (CaseServer.ts:161-170).
func TestCASEServerResumeMICFallsBack(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, _, err := r.open(ctx, r.nodeFabric); err != nil {
		t.Fatalf("CASE: %v", err)
	}
	r.accepted(t)
	addr := handshake.PeerAddress{FabricIndex: 3, NodeID: controllerNodeID}
	rec, _ := r.nodeStore.FindResumptionRecord(addr)
	rec.SharedSecret = bytes.Repeat([]byte{0x5A}, len(rec.SharedSecret))
	if err := r.nodeStore.SaveResumptionRecord(rec); err != nil {
		t.Fatal(err)
	}
	client, resumed, err := r.open(ctx, r.nodeFabric)
	if err != nil {
		t.Fatalf("CASE with a wrong secret: %v", err)
	}
	server := r.accepted(t)
	if resumed || server.Resumed() {
		t.Fatal("resumed although the MIC cannot decrypt")
	}
	ping(t, r.nodeMgr, client)
	if n := invalidParams(r.serverRec.unsecuredStatusReports()); n != 0 {
		t.Fatalf("%d InvalidParam reports sent, want none", n)
	}
}

// TestCASEServerResumptionPairRequired: a Sigma1 with a resumption id but
// no resume MIC is refused with InvalidParam (CaseServer.ts:108-111).
func TestCASEServerResumptionPairRequired(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	us, err := r.nodeMgr.NewUnsecuredSession(r.serverAddr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer us.Close()
	x, err := r.nodeMgr.Initiate(us, mrp.SecureChannelProtocolID, exchange.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	sigma1 := sigma.Sigma1{InitiatorSessionID: 7, InitiatorEphPubKey: bytes.Repeat([]byte{4}, 65), ResumptionID: bytes.Repeat([]byte{1}, 16)}
	if err := x.Send(ctx, mrp.SCOpcodeSigma1, sigma1.Marshal(), exchange.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	msg, err := x.Next(ctx, exchange.ReceiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	report, ok := exchange.DecodeStatusReport(msg.Payload)
	if msg.Opcode != mrp.SCOpcodeStatusReport || !ok || report.ProtocolCode != mrp.SCStatusProtocolInvalidParameter {
		t.Fatalf("answer 0x%02X %+v, want InvalidParam", msg.Opcode, report)
	}
}

// TestCASEServerClose: after Close the manager no longer routes Sigma1 to
// the responder; NewCASEServer refuses a missing resolver.
func TestCASEServerClose(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	r.server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := handshake.CASE(ctx, r.nodeMgr, r.serverAddr, r.nodeFabric, controllerNodeID, handshake.CASEOptions{}); err == nil {
		t.Fatal("CASE succeeded against a closed responder")
	}
	if _, err := handshake.NewCASEServer(r.serverMgr, handshake.CASEServerConfig{}); err == nil {
		t.Fatal("NewCASEServer without Fabrics accepted")
	}
}
