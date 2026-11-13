// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// failSafeRig is a bridge a host wired without the node facade: the root's
// GeneralCommissioning on a fake clock, a session registry that lists one
// PASE session and the CASE session commands arrive on. Nothing but the
// bridge's own wiring connects the fail-safe to the sessions.
type failSafeRig struct {
	b   *Bridge
	gc  *core.GeneralCommissioning
	clk *clocktest.Fake
	reg *sessionListingRegistry
	// send dispatches an ArmFailSafe on the CASE session and reads its
	// response.
	send func(exchangeID, expirySeconds uint16)
}

const (
	failSafeRigCASE uint16 = 91
	failSafeRigPASE uint16 = 77
)

// encodeArmFailSafe is an InvokeRequest of GeneralCommissioning.ArmFailSafe
// with the given expiry and Breadcrumb 1.
func encodeArmFailSafe(t *testing.T, expirySeconds uint16) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false) // SuppressResponse
	enc.PutBool(tlv.ContextTag(1), false) // TimedRequest
	enc.StartArray(tlv.ContextTag(2))
	enc.StartStruct(tlv.AnonymousTag())
	im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0030, Command: 0x00, HasEndpoint: true, HasCluster: true, HasCommand: true}.MarshalTLV(enc, tlv.ContextTag(0))
	enc.StartStruct(tlv.ContextTag(1))
	enc.PutUint16(tlv.ContextTag(0), expirySeconds)
	enc.PutUint(tlv.ContextTag(1), 1)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		t.Fatalf("encodeArmFailSafe: %v", err)
	}
	return body
}

func newFailSafeRig(t *testing.T) *failSafeRig {
	t.Helper()
	r := &failSafeRig{clk: clocktest.New(time.Unix(1000, 0))}
	r.b = newStartedBridge(t)
	r.b.AttachACLLister(endpoint.UnenforcedACL{})
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600, Clock: r.clk,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.gc = gc
	r.b.AttachRootClusters([]contract.ClusterServer{gc})
	r.reg = newSessionListingRegistry(
		operational.SessionInfo{SessionID: failSafeRigPASE, FabricIndex: 1, IsPASE: true},
		operational.SessionInfo{SessionID: failSafeRigCASE, FabricIndex: 1},
	)
	r.b.AttachSessionRegistry(r.reg)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, r.b, failSafeRigCASE)
	r.b.mu.RLock()
	inner := r.b.sessions
	r.b.mu.RUnlock()
	r.b.AttachSessionLookup(caseOnFabricLookup{SessionLookup: inner, id: failSafeRigCASE, fabric: 1})
	r.send = func(exchangeID, expirySeconds uint16) {
		t.Helper()
		request := encryptIMFromPeer(t, peerSess, failSafeRigCASE, message.ProtocolHeader{
			Opcode: im.OpcodeInvokeRequest, ExchangeID: exchangeID, Initiator: true, NeedsAck: true,
		}, encodeArmFailSafe(t, expirySeconds))
		if err := r.b.dispatch(context.Background(), request, peerAddr); err != nil {
			t.Fatalf("dispatch of ArmFailSafe(%d): %v", expirySeconds, err)
		}
		reply, _, ok := readOneIMDatagram(t, peerConn, peerSess, 5*time.Second)
		if !ok || reply.Opcode != im.OpcodeInvokeResponse {
			t.Fatalf("ArmFailSafe(%d): reply = (%v, opcode %#x), want the InvokeResponse", expirySeconds, ok, reply.Opcode)
		}
	}
	return r
}

// waitClosed waits until the registry closed a session and returns the
// distinct ids it closed.
func (r *failSafeRig) waitClosed(t *testing.T) []uint16 {
	t.Helper()
	r.reg.awaitClose(t)
	ids := r.reg.closedIDs()
	slices.Sort(ids)
	return slices.Compact(ids)
}

// TestFailSafeExpiryThroughTheBridgeClosesThePASESessions: the bridge
// clears the PASE sessions when the fail-safe of the root's
// GeneralCommissioning expires without a CommissioningComplete, for a host
// that wired nothing for it — no node facade, no fail-safe-expiry hook, no
// call of Bridge.FailSafeExpired. Mirrors matter.js
// packages/protocol/src/common/FailsafeContext.ts:277-279 at db021da0
// (rollback: "1. Terminate any open PASE secure session by clearing any
// associated Secure Session Context at the Server" → closePaseSession,
// :187-192). Before, the close was the host's call from its own hook, and a
// host that did not make it kept the commissioning channel's implicit
// Administer open (review II, R2-P0-sec-A03, owner decision 9).
func TestFailSafeExpiryThroughTheBridgeClosesThePASESessions(t *testing.T) {
	t.Parallel()
	t.Run("the timer", func(t *testing.T) {
		t.Parallel()
		r := newFailSafeRig(t)
		r.send(0x3001, 10)
		if !r.gc.FailSafeArmed() {
			t.Fatal("ArmFailSafe(10) did not arm the fail-safe: the test checks nothing")
		}
		// An armed fail-safe closes nothing: the commissioning it covers
		// is still running.
		r.clk.Advance(5 * time.Second)
		r.clk.Yield() // whatever the five seconds made runnable has run
		if got := r.reg.closedIDs(); len(got) != 0 {
			t.Fatalf("sessions closed while the fail-safe is armed = %v, want none", got)
		}
		r.clk.Advance(6 * time.Second)
		if got := r.waitClosed(t); !slices.Equal(got, []uint16{failSafeRigPASE}) {
			t.Fatalf("sessions closed after the fail-safe's timer = %v, want the PASE session [%d] alone", got, failSafeRigPASE)
		}
	})
	t.Run("ArmFailSafe with expiry 0", func(t *testing.T) {
		t.Parallel()
		r := newFailSafeRig(t)
		r.send(0x3002, 60)
		r.send(0x3003, 0)
		if r.gc.FailSafeArmed() {
			t.Fatal("ArmFailSafe(0) did not disarm the fail-safe: the test checks nothing")
		}
		if got := r.waitClosed(t); !slices.Equal(got, []uint16{failSafeRigPASE}) {
			t.Fatalf("sessions closed after ArmFailSafe(0) = %v, want the PASE session [%d] alone", got, failSafeRigPASE)
		}
	})
	t.Run("ExpireFailSafe", func(t *testing.T) {
		t.Parallel()
		r := newFailSafeRig(t)
		r.send(0x3004, 60)
		r.gc.ExpireFailSafe(context.Background())
		if got := r.waitClosed(t); !slices.Equal(got, []uint16{failSafeRigPASE}) {
			t.Fatalf("sessions closed after ExpireFailSafe = %v, want the PASE session [%d] alone", got, failSafeRigPASE)
		}
	})
	t.Run("a host hook that calls FailSafeExpired as well", func(t *testing.T) {
		t.Parallel()
		// The wiring the second wave documented: the host's own hook
		// calls Bridge.FailSafeExpired. It keeps working beside the
		// bridge's hook, on the timer too — whose context is the one of
		// the ArmFailSafe that armed it, an interaction long answered.
		r := newFailSafeRig(t)
		hostRan := make(chan struct{}, 1)
		r.gc.SetOnFailSafeExpired(func(ctx context.Context, _ uint8) {
			r.b.FailSafeExpired(ctx)
			hostRan <- struct{}{}
		})
		r.send(0x3005, 10)
		r.clk.Advance(11 * time.Second)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		select {
		case <-hostRan:
		case <-ctx.Done():
			t.Fatal("the host's fail-safe-expiry hook did not run")
		}
		if got := r.waitClosed(t); !slices.Equal(got, []uint16{failSafeRigPASE}) {
			t.Fatalf("sessions closed after the fail-safe's timer = %v, want the PASE session [%d] alone", got, failSafeRigPASE)
		}
	})
}

// TestFailSafeExpiredAfterItsInteractionClosesAtOnce holds the host's call
// on its own: Bridge.FailSafeExpired with the context of an interaction
// whose response is out — what a fail-safe's timer hands its hooks, the
// context of the ArmFailSafe that armed it — closes the sessions at once.
// Before, the close was registered with that interaction's spent
// after-response list and never ran.
func TestFailSafeExpiredAfterItsInteractionClosesAtOnce(t *testing.T) {
	t.Parallel()
	b, mgr, _ := paseCloseBridge(t)
	after := &im.AfterResponse{}
	ctx := im.WithAfterResponse(context.Background(), after)
	after.Run() // the interaction's response is out
	b.FailSafeExpired(ctx)
	if got := openPASESessions(mgr); got != 0 {
		t.Errorf("PASE sessions after the fail-safe expired = %d, want 0", got)
	}
	// A second call finds nothing and does nothing.
	b.FailSafeExpired(ctx)
	b.FailSafeExpired(context.Background())
	if got := openPASESessions(mgr); got != 0 {
		t.Errorf("PASE sessions after a repeated call = %d, want 0", got)
	}
}

// TestRevokeWindowThroughTheBridgeClosesThePASESessions: a window attached
// to the bridge closes the PASE sessions when it is revoked, for a host
// that wired no PaseSessionCloser. Mirrors matter.js
// packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:137-141
// at db021da0 (revokeCommissioning, "Step 1: Regardless of window state —
// terminate PASE sessions" → getPaseSession().initiateClose()).
func TestRevokeWindowThroughTheBridgeClosesThePASESessions(t *testing.T) {
	t.Parallel()
	t.Run("no closer wired", func(t *testing.T) {
		t.Parallel()
		b, mgr, _ := paseCloseBridge(t)
		w := NewCommissioningWindow()
		b.AttachCommissioningWindow(w)
		if err := w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		if got := openPASESessions(mgr); got != 0 {
			t.Errorf("PASE sessions after RevokeWindow = %d, want 0", got)
		}
	})
	t.Run("revoked over the PASE session itself", func(t *testing.T) {
		t.Parallel()
		// The close waits for the command's response
		// (TC-CADMIN-1.10 step 9).
		b, mgr, _ := paseCloseBridge(t)
		w := NewCommissioningWindow()
		b.AttachCommissioningWindow(w)
		after := &im.AfterResponse{}
		ctx := im.WithAfterResponse(im.WithAuthModePASE(context.Background()), after)
		if err := w.RevokeWindow(ctx); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		if got := openPASESessions(mgr); got != 1 {
			t.Fatalf("PASE sessions before the response = %d, want 1", got)
		}
		after.Run()
		if got := openPASESessions(mgr); got != 0 {
			t.Errorf("PASE sessions after the response = %d, want 0", got)
		}
	})
	t.Run("the host's closer still runs", func(t *testing.T) {
		t.Parallel()
		b, mgr, _ := paseCloseBridge(t)
		w := NewCommissioningWindow()
		host := &countingPaseCloser{}
		w.SetPaseSessionCloser(host)
		b.AttachCommissioningWindow(w)
		if err := w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		if host.calls != 1 {
			t.Errorf("the host's PaseSessionCloser ran %d times, want 1", host.calls)
		}
		if got := openPASESessions(mgr); got != 0 {
			t.Errorf("PASE sessions after RevokeWindow = %d, want 0 — the host's closer closed none, the bridge's must", got)
		}
	})
	t.Run("a window that was never attached", func(t *testing.T) {
		t.Parallel()
		// Without a bridge there are no sessions to own: the revoke is
		// what it was.
		w := NewCommissioningWindow()
		if err := w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
	})
}

// countingPaseCloser is a host's PaseSessionCloser that closes nothing.
type countingPaseCloser struct{ calls int }

func (c *countingPaseCloser) ClosePaseSessions(context.Context) error {
	c.calls++
	return nil
}
