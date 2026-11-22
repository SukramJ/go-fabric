// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/message"
)

// rollbackRig is a bridge with a real session table holding a
// commissioning's PASE session and a CASE session on the same fabric, the
// root's GeneralCommissioning wired by the bridge alone, and an ACL that
// grants Administer to the CASE subject only: a request on the PASE
// session is served only through the commissioning channel's implicit
// Administer grant.
type rollbackRig struct {
	b                 *Bridge
	gc                *core.GeneralCommissioning
	mgr               *operational.Manager
	peerConn          *net.UDPConn
	peers             map[uint16]*channel.Session
	pasePeer          *channel.Session
	casePeer          *channel.Session
	paseSession       uint16
	caseSession       uint16
	pasePeerSession   uint16
	casePeerSession   uint16
	exchange          uint16
	peerAddr          *net.UDPAddr
	afterRollbackHook func(ctx context.Context)
}

// newRollbackRig builds the rig; adopt puts the PASE session on the
// fabric, as AddNOC does.
func newRollbackRig(t *testing.T, adopt bool) *rollbackRig {
	t.Helper()
	const (
		bridgeNodeID uint64 = 0x0B0B
		peerNodeID   uint64 = 0x1234
		fabric       uint8  = 1
	)
	r := &rollbackRig{pasePeerSession: 0x0177, casePeerSession: 0x0191, exchange: 0x6000}
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", Clock: clk},
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	startCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })
	r.b = b
	b.AttachACLLister(caseAdministerACL{fabric: fabric, subject: peerNodeID})
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600, Clock: clk,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.gc = gc
	// The host's hook runs before the bridge's session closer
	// (failSafeExpiryHooksLocked): what it defers past the response runs
	// before a close the closer defers.
	gc.SetOnFailSafeExpiry(func(ctx context.Context, _ core.FailSafeExpiry) {
		if r.afterRollbackHook != nil {
			r.afterRollbackHook(ctx)
		}
	})
	b.AttachRootClusters([]contract.ClusterServer{gc})

	mgr := operational.NewManager(nil)
	mgr.SetClock(clk)
	mgr.SetOnSessionClose(b.SessionClosed)
	b.AttachSessionRegistry(mgr)
	r.mgr = mgr
	b.AttachSessionLookup(NewOperationalSessionLookup(func(id uint16) (*channel.Session, bool) {
		if e, ok := r.open(id); ok {
			return e.Session, true
		}
		return nil, false
	}).WithFabricResolver(func(id uint16) (uint8, bool) {
		if e, ok := r.open(id); ok {
			return e.FabricIndex(), true
		}
		return 0, false
	}).WithSubjectResolver(func(id uint16) (uint64, []uint32, bool) {
		if e, ok := r.open(id); ok && e.Session != nil {
			return e.Session.PeerNodeID(), e.Session.PeerCATs(), true
		}
		return 0, nil, false
	}).WithPASEResolver(func(id uint16) (bool, bool) {
		if e, ok := r.open(id); ok {
			return e.IsPASE(), true
		}
		return false, false
	}))

	secret := []byte("pase-shared-secret")
	paseStake, err := mgr.AllocateStake()
	if err != nil {
		t.Fatal(err)
	}
	r.paseSession = paseStake.ID
	if _, err := mgr.OpenFromPaseWithStake(paseStake, bridgeNodeID, peerNodeID+1, r.pasePeerSession, secret); err != nil {
		t.Fatalf("OpenFromPaseWithStake: %v", err)
	}
	if adopt {
		if err := mgr.AdoptFabricIndex(r.paseSession, fabric); err != nil {
			t.Fatalf("AdoptFabricIndex: %v", err)
		}
	}
	derived, err := hkdf.Key(sha256.New, secret, nil, "SessionKeys", 48)
	if err != nil {
		t.Fatal(err)
	}
	r.pasePeer, err = channel.New(channel.Config{
		EncryptKey: derived[0:16], DecryptKey: derived[16:32],
		LocalNodeID: peerNodeID + 1, PeerNodeID: bridgeNodeID, InitialCounter: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var keys sigma.SessionKeys
	for i := range keys.I2RKey {
		keys.I2RKey[i], keys.R2IKey[i] = byte(0x10+i), byte(0x60+i)
	}
	caseStake, err := mgr.AllocateStake()
	if err != nil {
		t.Fatal(err)
	}
	r.caseSession = caseStake.ID
	if _, err := mgr.OpenFromSigmaWithStake(caseStake, fabric, bridgeNodeID, peerNodeID, r.casePeerSession, nil, keys); err != nil {
		t.Fatalf("OpenFromSigmaWithStake: %v", err)
	}
	r.casePeer, err = channel.New(channel.Config{
		EncryptKey: keys.I2RKey[:], DecryptKey: keys.R2IKey[:],
		LocalNodeID: peerNodeID, PeerNodeID: bridgeNodeID, InitialCounter: 4000,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.peers = map[uint16]*channel.Session{r.pasePeerSession: r.pasePeer, r.casePeerSession: r.casePeer}
	r.peerConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = r.peerConn.Close() })
	r.peerAddr, _ = r.peerConn.LocalAddr().(*net.UDPAddr)
	return r
}

// open reports the session table's entry for id, if it is open.
func (r *rollbackRig) open(id uint16) (*operational.Entry, bool) {
	e, err := r.mgr.Get(id)
	return e, err == nil && e != nil
}

// sendOver dispatches payload on the PASE session (pase) or the CASE one.
func (r *rollbackRig) sendOver(t *testing.T, pase bool, payload []byte) error {
	t.Helper()
	r.exchange++
	peer, session := r.casePeer, r.caseSession
	if pase {
		peer, session = r.pasePeer, r.paseSession
	}
	return r.b.dispatch(context.Background(), encryptIMFromPeer(t, peer, session, message.ProtocolHeader{
		Opcode: im.OpcodeInvokeRequest, ExchangeID: r.exchange, Initiator: true, NeedsAck: true,
	}, payload), r.peerAddr)
}

// invokeResponsesOn counts the InvokeResponses among got that went out on
// the peer session peerSession.
func invokeResponsesOn(got []peerDatagram, peerSession uint16) int {
	n := 0
	for _, d := range got {
		if d.sessionID == peerSession && d.proto.ProtocolID == im.InteractionModelProtocolID &&
			d.proto.Opcode == im.OpcodeInvokeResponse {
			n++
		}
	}
	return n
}

// paseProbe is what a request on the PASE session got when it was sent in
// the window between the rollback's handler and its response's
// after-response work.
type paseProbe struct {
	sessionOpen bool  // the PASE session was still in the table when the response was out
	err         error // the probe's dispatch error
}

// TestArmFailSafeZeroClosesThePASESessionByTheExchangeItArrivedOn pins
// when the rollback of ArmFailSafe with expiry 0 closes the PASE session.
// matter.js hands the command's exchange down
// (packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:90-95,
// packages/protocol/src/common/FailsafeTimer.ts:61-65,
// packages/protocol/src/common/FailsafeContext.ts:283 at b25ed960) and
// the force close defers only for an exchange of the PASE session itself
// (packages/protocol/src/session/Session.ts:336-339, :315-317 at
// b25ed960): over CASE the PASE session is closed inside the handler,
// before the response; over the PASE session it closes once the response
// is out. The bridge deferred the close past the response for every
// interaction (F-b25ed960-54).
func TestArmFailSafeZeroClosesThePASESessionByTheExchangeItArrivedOn(t *testing.T) {
	t.Parallel()
	// probeAfterResponse arms the rig so that, when the rollback runs, a
	// request on the PASE session — ArmFailSafe(60), which needs
	// Administer — is sent once the rollback's response is out and before
	// any close the bridge deferred past it.
	probeAfterResponse := func(t *testing.T, r *rollbackRig) <-chan paseProbe {
		t.Helper()
		got := make(chan paseProbe, 1)
		r.afterRollbackHook = func(ctx context.Context) {
			if !im.DeferAfterResponse(ctx, func() {
				_, open := r.open(r.paseSession)
				done := make(chan error, 1)
				go func() { done <- r.sendOver(t, true, encodeArmFailSafe(t, 60)) }()
				wait, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				select {
				case err := <-done:
					got <- paseProbe{sessionOpen: open, err: err}
				case <-wait.Done():
					t.Error("the probe on the PASE session did not return")
					got <- paseProbe{sessionOpen: open, err: wait.Err()}
				}
			}) {
				t.Error("the rollback's context carries no pending after-response list: the test checks nothing")
			}
		}
		return got
	}

	t.Run("over CASE, closed before the response", func(t *testing.T) {
		t.Parallel()
		r := newRollbackRig(t, true)
		// The commissioning's fail-safe, moved onto the fabric by AddNOC.
		if err := r.gc.ArmFailSafeFor(context.Background(), 60, 1); err != nil {
			t.Fatalf("ArmFailSafeFor: %v", err)
		}
		probe := probeAfterResponse(t, r)
		if err := r.sendOver(t, false, encodeArmFailSafe(t, 0)); err != nil {
			t.Fatalf("dispatch of ArmFailSafe(0) over CASE: %v", err)
		}
		p := <-probe
		got := readPeerDatagrams(t, r.peerConn, 300*time.Millisecond, r.peers)
		if n := invokeResponsesOn(got, r.casePeerSession); n != 1 {
			t.Fatalf("InvokeResponses on the CASE session = %d, want the ArmFailSafeResponse: %+v", n, got)
		}
		// M1: the PASE session was gone by the time the response was out.
		if p.sessionOpen {
			t.Errorf("the PASE session %d was still open after the ArmFailSafeResponse went out over CASE; matter.js closes it inside the handler", r.paseSession)
		}
		// M2: a request on it in that window is not served.
		if !errors.Is(p.err, ErrSessionMissing) {
			t.Errorf("a request on the PASE session after the rollback = %v, want ErrSessionMissing", p.err)
		}
		if n := invokeResponsesOn(got, r.pasePeerSession); n != 0 {
			t.Errorf("the bridge answered %d request(s) on the PASE session after the rollback closed it", n)
		}
		if r.gc.FailSafeArmed() {
			t.Error("a request on the PASE session re-armed the fail-safe its rollback had just ended")
		}
		if _, ok := r.open(r.caseSession); !ok {
			t.Error("the CASE session the command arrived on was closed")
		}
	})

	t.Run("over the PASE session, closed after the response", func(t *testing.T) {
		t.Parallel()
		r := newRollbackRig(t, false)
		if err := r.sendOver(t, true, encodeArmFailSafe(t, 60)); err != nil {
			t.Fatalf("dispatch of ArmFailSafe(60) over PASE: %v", err)
		}
		if !r.gc.FailSafeArmed() {
			t.Fatal("ArmFailSafe(60) over PASE did not arm the fail-safe: the test checks nothing")
		}
		_ = readPeerDatagrams(t, r.peerConn, 300*time.Millisecond, r.peers)
		var openAfterResponse bool
		r.afterRollbackHook = func(ctx context.Context) {
			if !im.DeferAfterResponse(ctx, func() { _, openAfterResponse = r.open(r.paseSession) }) {
				t.Error("the rollback's context carries no pending after-response list: the test checks nothing")
			}
		}
		if err := r.sendOver(t, true, encodeArmFailSafe(t, 0)); err != nil {
			t.Fatalf("dispatch of ArmFailSafe(0) over PASE: %v", err)
		}
		got := readPeerDatagrams(t, r.peerConn, 300*time.Millisecond, r.peers)
		if n := invokeResponsesOn(got, r.pasePeerSession); n != 1 {
			t.Fatalf("InvokeResponses on the PASE session = %d, want the ArmFailSafeResponse: %+v", n, got)
		}
		if !openAfterResponse {
			t.Error("the PASE session closed before its own ArmFailSafeResponse was out")
		}
		if _, ok := r.open(r.paseSession); ok {
			t.Errorf("the PASE session %d is still open after the response of its rollback", r.paseSession)
		}
	})
}
