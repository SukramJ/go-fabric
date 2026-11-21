// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// revokePASEDevice is a started bridge with one un-adopted PASE session
// (fabric index 0, as TC-CADMIN-1.10's) whose peer is a UDP socket the
// bridge has a route to, and a window attached to the bridge.
func revokePASEDevice(t *testing.T) (dev *resumeDevice, w *CommissioningWindow, sid, peerSID uint16, peer net.PacketConn) {
	t.Helper()
	dev = startResumeDevice(t, newMemSubscriptionStore(), nil)
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	const peerSessionID = uint16(0x0E07)
	e, err := dev.sessions.OpenFromPase(1, 2, peerSessionID, []byte("pase-shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	dev.br.bindSessionRoute(e.SessionID, peer.LocalAddr())
	w = NewCommissioningWindow()
	dev.br.AttachCommissioningWindow(w)
	return dev, w, e.SessionID, peerSessionID, peer
}

// facadePaseCloser is what the node facade wires as the window's
// PaseSessionCloser (node/commissioning.go paseCloser): a silent close of
// every fabric-0 session on the same manager.
type facadePaseCloser struct{ sessions *operational.Manager }

func (c facadePaseCloser) ClosePaseSessions(context.Context) error {
	c.sessions.CloseFabric(0)
	return nil
}

func assertPASEClosed(t *testing.T, mgr *operational.Manager, sid uint16) {
	t.Helper()
	if _, err := mgr.Get(sid); !errors.Is(err, operational.ErrSessionNotFound) {
		t.Errorf("PASE session %#x after the revoke: %v, want closed", sid, err)
	}
}

// TestRevokeWindowSendsCloseSessionOnThePASESession: the revoke's close of
// the PASE session is graceful — the peer is told with a CloseSession
// (StatusReport Success/CloseSession, no ack requested; wire shape held by
// TestSendCloseSessionReport_WireShapeMatchesMatterJS) on that session
// before it closes, instead of learning by timeout. Mirrors matter.js
// packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:143-147
// at b25ed960 (initiateClose), whose gracefulClose
// packages/protocol/src/protocol/ExchangeManager.ts:757-768 at b25ed960
// observes and answers with
// packages/protocol/src/protocol/ExchangeManager.ts:790-800 at b25ed960
// (#sendCloseSession) (F-b25ed960-56 (ii)).
func TestRevokeWindowSendsCloseSessionOnThePASESession(t *testing.T) {
	t.Parallel()
	t.Run("revoked over CASE", func(t *testing.T) {
		t.Parallel()
		dev, w, sid, peerSID, peer := revokePASEDevice(t)
		if err := w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		if got := readCloseSessionTargets(t, peer); len(got) != 1 || got[0] != peerSID {
			t.Fatalf("CloseSession sent on peer sessions %#x, want [%#x]", got, peerSID)
		}
		assertPASEClosed(t, dev.sessions, sid)
	})
	t.Run("revoked over the PASE session itself", func(t *testing.T) {
		t.Parallel()
		// The close, and with it the CloseSession, follows the
		// command's response (matter.js NodeSession.ts:455-462 at
		// b25ed960: the deferred close runs when the last exchange ends).
		dev, w, sid, peerSID, peer := revokePASEDevice(t)
		after := &im.AfterResponse{}
		ctx := im.WithAfterResponse(im.WithAuthModePASE(core.WithInvokeSessionID(context.Background(), sid)), after)
		if err := w.RevokeWindow(ctx); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		if got := readCloseSessionTargets(t, peer); len(got) != 0 {
			t.Fatalf("datagrams before the response on peer sessions %#x, want none", got)
		}
		if _, err := dev.sessions.Get(sid); err != nil {
			t.Fatalf("PASE session before the response: %v, want open", err)
		}
		after.Run()
		if got := readCloseSessionTargets(t, peer); len(got) != 1 || got[0] != peerSID {
			t.Fatalf("CloseSession sent on peer sessions %#x, want [%#x]", got, peerSID)
		}
		assertPASEClosed(t, dev.sessions, sid)
	})
	t.Run("the host's closer runs first", func(t *testing.T) {
		t.Parallel()
		// The node facade's closer is a silent CloseFabric(0) and the
		// window runs the host's closer before the bridge's: the
		// CloseSession must still go out.
		dev, w, sid, peerSID, peer := revokePASEDevice(t)
		w.SetPaseSessionCloser(facadePaseCloser{dev.sessions})
		if err := w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		if got := readCloseSessionTargets(t, peer); len(got) != 1 || got[0] != peerSID {
			t.Fatalf("CloseSession sent on peer sessions %#x, want [%#x]", got, peerSID)
		}
		assertPASEClosed(t, dev.sessions, sid)
	})
}
