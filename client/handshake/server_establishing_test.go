// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// nodeAddr is the node of the responder rig as the controller's peer.
var nodeAddr = exchange.PeerAddress{FabricIndex: 1, NodeID: testNodeID}

// isSuccess reports a datagram that is the responder's Success
// StatusReport on the unsecured session.
func isSuccess(d []byte) bool {
	opcode, payload, ok := unsecuredMessage(d)
	if !ok || opcode != mrp.SCOpcodeStatusReport {
		return false
	}
	r, ok := exchange.DecodeStatusReport(payload)
	return ok && r.GeneralCode == mrp.SCStatusGeneralSuccess && r.ProtocolCode == mrp.SCStatusProtocolSessionEstablishmentSuccess
}

// TestCASEServerResponderSessionEstablishing: the CASE responder's session
// is registered establishing and established when its Success first went
// out (full handshake) or the initiator's Success arrived (resumption) —
// matter.js CaseServer.ts:177-211, :307-357 at b25ed960.
func TestCASEServerResponderSessionEstablishing(t *testing.T) {
	t.Parallel()
	t.Run("is listed but not handed over while the Success goes out", func(t *testing.T) {
		t.Parallel()
		// SessionsFor (routing, peer loss, close) sees the session while
		// it establishes; OnSession, which feeds the peer set — the
		// established view — runs only after the establishment
		// (SessionManager.ts:702-705, PeerSet.ts:104-110 at b25ed960).
		r := newResponderRig(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		type look struct{ listed, handedOver bool }
		looks := make(chan look, 8)
		r.serverRec.setHook(func(d []byte) {
			if isSuccess(d) {
				looks <- look{listed: len(r.serverMgr.SessionsFor(nodeAddr)) == 1, handedOver: len(r.sessions) > 0}
			}
		})
		if _, _, err := r.open(ctx, r.nodeFabric); err != nil {
			t.Fatalf("CASE: %v", err)
		}
		server := r.accepted(t)
		l := <-looks
		if !l.listed || l.handedOver {
			t.Fatalf("at the Success: listed %v, handed over %v; want listed and not handed over", l.listed, l.handedOver)
		}
		if got := r.serverMgr.SessionsFor(nodeAddr); len(got) != 1 || got[0] != server {
			t.Fatalf("SessionsFor = %v", got)
		}
	})
	t.Run("keeps the session and saves its record when the send of the Success fails after it went out", func(t *testing.T) {
		t.Parallel()
		// Once the Success went out the initiator may use the session, so
		// a later failure of its send does not close it
		// (CaseServer.ts:337-342 at b25ed960; decision 288). The send
		// fails in its ack wait: the node's acknowledgements are lost and
		// the responder is closed, which ends the wait.
		r := newResponderRig(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		r.nodeRec.setRefuse(func(d []byte) error {
			if opcode, _, ok := unsecuredMessage(d); ok && opcode == mrp.StandaloneAckOpcode {
				return errors.New("test: the Success's acknowledgement is lost")
			}
			return nil
		})
		wentOut := make(chan struct{}, 8)
		r.serverRec.setHook(func(d []byte) {
			if isSuccess(d) {
				wentOut <- struct{}{}
			}
		})
		if _, _, err := r.open(ctx, r.nodeFabric); err != nil {
			t.Fatalf("CASE: %v", err)
		}
		<-wentOut
		r.server.Close()
		var server *exchange.Session
		select {
		case server = <-r.sessions:
		default:
			t.Fatal("the session whose Success went out was not handed over")
		}
		if server.IsClosing() {
			t.Fatal("the session closed")
		}
		if _, ok := r.store.FindResumptionRecord(handshake.PeerAddress{FabricIndex: 1, NodeID: testNodeID}); !ok {
			t.Fatal("no resumption record saved")
		}
	})
	t.Run("closes the session when the first transmission of the Success fails", func(t *testing.T) {
		t.Parallel()
		r := newResponderRig(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		refused := make(chan struct{}, 8)
		r.serverRec.setRefuse(func(d []byte) error {
			if isSuccess(d) {
				refused <- struct{}{}
				return errors.New("test: the Success is not sent")
			}
			return nil
		})
		opened := make(chan struct{})
		go func() {
			defer close(opened)
			_, _, _ = r.open(ctx, r.nodeFabric)
		}()
		defer func() { cancel(); <-opened }()
		select {
		case <-refused:
		case <-ctx.Done():
			t.Fatal("no Success attempted")
		}
		deadline := time.Now().Add(20 * time.Second)
		for len(r.serverMgr.SessionsFor(nodeAddr)) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("the session whose Success never went out is still registered")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if len(r.sessions) != 0 {
			t.Fatal("the session was handed over")
		}
		if _, ok := r.store.FindResumptionRecord(handshake.PeerAddress{FabricIndex: 1, NodeID: testNodeID}); ok {
			t.Fatal("a resumption record was saved")
		}
	})
	t.Run("refuses a resumed session its fabric's teardown closed while it established", func(t *testing.T) {
		t.Parallel()
		// The fabric's sessions are closed between Sigma2_Resume and the
		// initiator's Success: the session is never established, not
		// handed over, its record not moved, and the handshake answered
		// with InvalidParam — CaseServer.ts:204-211, :367-376, :84-96 at
		// b25ed960 with Fabric.ts:493-496.
		r := newResponderRig(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		if _, _, err := r.open(ctx, r.nodeFabric); err != nil {
			t.Fatalf("CASE: %v", err)
		}
		first := r.accepted(t)
		before, _ := r.store.FindResumptionRecord(handshake.PeerAddress{FabricIndex: 1, NodeID: testNodeID})
		r.serverRec.setHook(func(d []byte) {
			if opcode, _, ok := unsecuredMessage(d); ok && opcode == mrp.SCOpcodeSigma2Resume {
				for _, s := range r.serverMgr.SessionsFor(nodeAddr) {
					s.Close()
				}
			}
		})
		if _, resumed, err := r.open(ctx, r.nodeFabric); err != nil || !resumed {
			t.Fatalf("resumed CASE: resumed %v, %v", resumed, err)
		}
		invalidParam := func() bool {
			return statusCount(r.serverRec.unsecuredStatusReports(), mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolInvalidParameter) == 1
		}
		deadline := time.Now().Add(20 * time.Second)
		for !invalidParam() {
			if time.Now().After(deadline) {
				t.Fatalf("no InvalidParam; reports %+v", r.serverRec.unsecuredStatusReports())
			}
			time.Sleep(10 * time.Millisecond)
		}
		if len(r.sessions) != 0 {
			t.Fatal("the session closed during its establishment was handed over")
		}
		if !first.IsClosed() || slices.ContainsFunc(r.serverMgr.SessionsFor(nodeAddr), func(s *exchange.Session) bool { return !s.IsClosing() }) {
			t.Fatal("a session of the torn-down fabric is still open")
		}
		after, _ := r.store.FindResumptionRecord(handshake.PeerAddress{FabricIndex: 1, NodeID: testNodeID})
		if !bytes.Equal(after.ResumptionID, before.ResumptionID) {
			t.Fatal("the refused resumption moved the record")
		}
	})
}
