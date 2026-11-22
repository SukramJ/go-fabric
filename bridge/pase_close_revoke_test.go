// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// revokePeer is the commissioner's side of the PASE session: its socket
// and its half of the session, keyed by the peer's session id.
type revokePeer struct {
	conn  *net.UDPConn
	peers map[uint16]*channel.Session
}

// read decrypts what reaches the commissioner until it stays quiet.
func (p revokePeer) read(t *testing.T) []peerDatagram {
	t.Helper()
	return readPeerDatagrams(t, p.conn, 300*time.Millisecond, p.peers)
}

// revokePASEDevice is a started bridge with one un-adopted PASE session
// (fabric index 0, as TC-CADMIN-1.10's) whose peer is a UDP socket the
// bridge has a route to, and a window attached to the bridge.
func revokePASEDevice(t *testing.T) (dev *resumeDevice, w *CommissioningWindow, sid, peerSID uint16, peer revokePeer) {
	t.Helper()
	return revokePASEDeviceWith(t, nil)
}

// revokePASEDeviceWith is [revokePASEDevice] with configure run before the
// device starts.
func revokePASEDeviceWith(t *testing.T, configure func(*Bridge, *operational.Manager)) (dev *resumeDevice, w *CommissioningWindow, sid, peerSID uint16, peer revokePeer) {
	t.Helper()
	dev = startResumeDevice(t, newMemSubscriptionStore(), configure)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	const (
		peerSessionID = uint16(0x0E07)
		bridgeNodeID  = uint64(1)
		peerNodeID    = uint64(2)
	)
	secret := []byte("pase-shared-secret")
	e, err := dev.sessions.OpenFromPase(bridgeNodeID, peerNodeID, peerSessionID, secret)
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	derived, err := hkdf.Key(sha256.New, secret, nil, "SessionKeys", 48)
	if err != nil {
		t.Fatal(err)
	}
	peerSess, err := channel.New(channel.Config{
		EncryptKey: derived[0:16], DecryptKey: derived[16:32],
		LocalNodeID: peerNodeID, PeerNodeID: bridgeNodeID, InitialCounter: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	dev.br.bindSessionRoute(e.SessionID, conn.LocalAddr())
	w = NewCommissioningWindow()
	dev.br.AttachCommissioningWindow(w)
	return dev, w, e.SessionID, peerSessionID, revokePeer{conn: conn, peers: map[uint16]*channel.Session{peerSessionID: peerSess}}
}

// assertOneCloseSession holds got to exactly one datagram, on peerSID: a
// Secure Channel StatusReport (GeneralCode Success, ProtocolId Secure
// Channel, ProtocolCode CloseSession) that requests no ack.
func assertOneCloseSession(t *testing.T, got []peerDatagram, peerSID uint16) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("%d datagrams reached the commissioner, want exactly one CloseSession: %+v", len(got), got)
	}
	d := got[0]
	if d.sessionID != peerSID {
		t.Errorf("datagram on peer session %#x, want %#x", d.sessionID, peerSID)
	}
	if d.proto.ProtocolID != mrp.SecureChannelProtocolID || d.proto.Opcode != mrp.SCOpcodeStatusReport {
		t.Fatalf("protocol %#x opcode %#x, want Secure Channel StatusReport", d.proto.ProtocolID, d.proto.Opcode)
	}
	if d.proto.NeedsAck {
		t.Error("NeedsAck = true, want false (no ack requested)")
	}
	if len(d.body) != 8 {
		t.Fatalf("StatusReport body %d bytes, want 8", len(d.body))
	}
	if general := binary.LittleEndian.Uint16(d.body[0:2]); general != mrp.SCStatusGeneralSuccess {
		t.Errorf("GeneralCode = %#x, want Success", general)
	}
	if pid := binary.LittleEndian.Uint32(d.body[2:6]); pid != uint32(mrp.SecureChannelProtocolID) {
		t.Errorf("ProtocolId = %#x, want Secure Channel", pid)
	}
	if code := binary.LittleEndian.Uint16(d.body[6:8]); code != scStatusProtocolCloseSession {
		t.Errorf("ProtocolCode = %#x, want CloseSession", code)
	}
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
		assertOneCloseSession(t, peer.read(t), peerSID)
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
		if got := peer.read(t); len(got) != 0 {
			t.Fatalf("datagrams before the response: %+v, want none", got)
		}
		if _, err := dev.sessions.Get(sid); err != nil {
			t.Fatalf("PASE session before the response: %v, want open", err)
		}
		after.Run()
		assertOneCloseSession(t, peer.read(t), peerSID)
		assertPASEClosed(t, dev.sessions, sid)
	})
	t.Run("the host's closer runs after the farewell", func(t *testing.T) {
		t.Parallel()
		// A silent CloseFabric(0) as the host's closer runs after the
		// bridge closed the idle PASE session with its CloseSession: the
		// farewell still goes out.
		dev, w, sid, peerSID, peer := revokePASEDevice(t)
		w.SetPaseSessionCloser(facadePaseCloser{dev.sessions})
		if err := w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		assertOneCloseSession(t, peer.read(t), peerSID)
		assertPASEClosed(t, dev.sessions, sid)
	})
}
