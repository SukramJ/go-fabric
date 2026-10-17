// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// readCloseSessionTargets collects the header session ids of the
// datagrams peer receives until it stays quiet for a moment.
func readCloseSessionTargets(t *testing.T, peer net.PacketConn) []uint16 {
	t.Helper()
	var ids []uint16
	buf := make([]byte, 2048)
	for {
		_ = peer.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, _, err := peer.ReadFrom(buf)
		if err != nil {
			return ids
		}
		hdr, _, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatalf("datagram header: %v", err)
		}
		ids = append(ids, hdr.SessionID)
	}
}

// TestCloseFabricSessionsExcept_TellsEachOtherSession: the NOC-rotation
// path tells every other session of the rotated fabric with a CloseSession
// and closes it; the invoking session and another fabric's session stay
// open and are not told (matter.js Fabric.ts:408-415 replaced →
// initiateClose → gracefulClose → ExchangeManager.ts:716-726).
func TestCloseFabricSessionsExcept_TellsEachOtherSession(t *testing.T) {
	t.Parallel()
	dev := startResumeDevice(t, newMemSubscriptionStore(), nil)
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	peerAddr, ok := peer.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address %T", peer.LocalAddr())
	}
	const (
		invoking = uint16(0x0F01)
		sibling  = uint16(0x0F02)
		unrouted = uint16(0x0F03) // never carried a message: nowhere to tell it
		other    = uint16(0x0F04) // another fabric
	)
	for _, s := range []struct {
		sid    uint16
		fabric uint8
	}{{invoking, resumeFabricIndex}, {sibling, resumeFabricIndex}, {unrouted, resumeFabricIndex}, {other, resumeFabricIndex + 1}} {
		if _, err := dev.sessions.OpenFromSigmaWithID(s.sid, s.fabric, resumeDeviceNode, resumeCtrlNode+uint64(s.sid), s.sid+0x100, nil, peerLossKeys(t)); err != nil {
			t.Fatal(err)
		}
		if s.sid != unrouted {
			dev.br.bindSessionRoute(s.sid, peerAddr)
		}
	}

	dev.br.CloseFabricSessionsExcept(resumeFabricIndex, invoking)

	if got := readCloseSessionTargets(t, peer); len(got) != 1 || got[0] != sibling+0x100 {
		t.Fatalf("CloseSession sent on peer sessions %#x, want [%#x]", got, sibling+0x100)
	}
	for _, sid := range []uint16{sibling, unrouted} {
		if _, err := dev.sessions.Get(sid); !errors.Is(err, operational.ErrSessionNotFound) {
			t.Errorf("session %#x after the rotation: %v, want closed", sid, err)
		}
	}
	for _, sid := range []uint16{invoking, other} {
		if _, err := dev.sessions.Get(sid); err != nil {
			t.Errorf("session %#x must stay open: %v", sid, err)
		}
	}
}

// TestEmitFabricRemovedContext_InvokingSessionLast: inside the command the
// other sessions are told and closed at once, the invoking session closes
// after the response without a CloseSession; outside a command every
// session is told and closed (matter.js Fabric.ts:423-432, :461-465,
// NodeSession.ts:349-352).
func TestEmitFabricRemovedContext_InvokingSessionLast(t *testing.T) {
	t.Parallel()
	dev := startResumeDevice(t, newMemSubscriptionStore(), nil)
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	peerAddr, ok := peer.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address %T", peer.LocalAddr())
	}
	const (
		invoking = uint16(0x1101)
		sibling  = uint16(0x1102)
	)
	for _, sid := range []uint16{invoking, sibling} {
		if _, err := dev.sessions.OpenFromSigmaWithID(sid, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode+uint64(sid), sid+0x100, nil, peerLossKeys(t)); err != nil {
			t.Fatal(err)
		}
		dev.br.bindSessionRoute(sid, peerAddr)
	}
	after := &im.AfterResponse{}
	ctx := mattercore.WithInvokeSessionID(im.WithAfterResponse(context.Background(), after), invoking)
	dev.br.EmitFabricRemovedContext(ctx, resumeFabricIndex)
	if _, err := dev.sessions.Get(sibling); !errors.Is(err, operational.ErrSessionNotFound) {
		t.Errorf("the other session before the response: %v, want closed", err)
	}
	if _, err := dev.sessions.Get(invoking); err != nil {
		t.Fatalf("the invoking session closed before its response: %v", err)
	}
	after.Run()
	if _, err := dev.sessions.Get(invoking); !errors.Is(err, operational.ErrSessionNotFound) {
		t.Errorf("the invoking session after the response: %v, want closed", err)
	}
	if got := readCloseSessionTargets(t, peer); len(got) != 1 || got[0] != sibling+0x100 {
		t.Fatalf("CloseSession sent on peer sessions %#x, want only [%#x]", got, sibling+0x100)
	}

	// Outside a command: every session of the fabric is told.
	const lone = uint16(0x1103)
	if _, err := dev.sessions.OpenFromSigmaWithID(lone, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, lone+0x100, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}
	dev.br.bindSessionRoute(lone, peerAddr)
	dev.br.EmitFabricRemovedContext(context.Background(), resumeFabricIndex)
	if got := readCloseSessionTargets(t, peer); len(got) != 1 || got[0] != lone+0x100 {
		t.Fatalf("CloseSession outside a command on %#x, want [%#x]", got, lone+0x100)
	}
	if _, err := dev.sessions.Get(lone); !errors.Is(err, operational.ErrSessionNotFound) {
		t.Errorf("session outside a command: %v, want closed", err)
	}
}

// TestCloseFabricSessionsExcept_WithoutCapability: a registry that cannot
// close by fabric leaves the sessions alone and sends nothing.
func TestCloseFabricSessionsExcept_WithoutCapability(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	reg := &fakeSessionRegistry{}
	b.AttachSessionRegistry(reg)
	b.CloseFabricSessionsExcept(1, 0)
	b.EmitFabricRemovedContext(context.Background(), 1)
	if got := reg.closedSessions(); len(got) != 0 {
		t.Fatalf("sessions closed without the capability: %v", got)
	}
	b.notifyFabricSessionsClosing(0, 0) // fabric 0 (PASE) is never told
}

// TestCloseFabricSessionsAfterUpdate_ClosesTheInvokingSessionToo: after
// UpdateNOC the invoking session closes as well — after its response
// where the IM layer defers it, at once otherwise — without a
// CloseSession, while the fabric's other sessions are told (matter.js
// Fabric.ts:408-415 replaced → initiateForceClose, NodeSession.ts:349-352).
func TestCloseFabricSessionsAfterUpdate_ClosesTheInvokingSessionToo(t *testing.T) {
	t.Parallel()
	dev := startResumeDevice(t, newMemSubscriptionStore(), nil)
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	peerAddr, ok := peer.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address %T", peer.LocalAddr())
	}
	const (
		invoking = uint16(0x0E01)
		sibling  = uint16(0x0E02)
	)
	for _, sid := range []uint16{invoking, sibling} {
		if _, err := dev.sessions.OpenFromSigmaWithID(sid, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode+uint64(sid), sid+0x100, nil, peerLossKeys(t)); err != nil {
			t.Fatal(err)
		}
		dev.br.bindSessionRoute(sid, peerAddr)
	}
	dev.br.CloseFabricSessionsAfterUpdate(mattercore.WithInvokeSessionID(context.Background(), invoking), resumeFabricIndex)
	if got := readCloseSessionTargets(t, peer); len(got) != 1 || got[0] != sibling+0x100 {
		t.Fatalf("CloseSession sent on peer sessions %#x, want only the sibling's [%#x]", got, sibling+0x100)
	}
	for _, sid := range []uint16{invoking, sibling} {
		if _, err := dev.sessions.Get(sid); !errors.Is(err, operational.ErrSessionNotFound) {
			t.Errorf("session %#x after UpdateNOC: %v, want closed", sid, err)
		}
	}
}

// establishedReporter is a handshake handler that names the session it
// established.
type establishedReporter uint16

func (e establishedReporter) establishedSession() (uint16, bool) { return uint16(e), true }

// TestSettleHandshake_BindsUDPSessionAtEstablishment: a session a UDP
// handshake established has its peer address from the handshake on, so a
// CloseSession can reach it before it carried an authenticated message
// (matter.js creates the session on the handshake's channel,
// CaseServer.ts:175-176, :316-317).
func TestSettleHandshake_BindsUDPSessionAtEstablishment(t *testing.T) {
	t.Parallel()
	dev := startResumeDevice(t, newMemSubscriptionStore(), nil)
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 47000}
	ok := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), 0, nil)
	dev.br.settleHandshake(src, 0x51, establishedReporter(0x0D01), mrp.SCOpcodeStatusReport, ok, nil)
	if got := dev.br.resolveSessionPeerAddr(0x0D01); got == nil || got.String() != src.String() {
		t.Fatalf("peer address of the established UDP session = %v, want %v", got, src)
	}
	// A failed or unfinished step binds nothing.
	dev.br.settleHandshake(src, 0x52, establishedReporter(0x0D02), mrp.SCOpcodeSigma2, []byte{1}, nil)
	if got := dev.br.resolveSessionPeerAddr(0x0D02); got != nil {
		t.Errorf("a Sigma2 bound session 0x0D02 to %v", got)
	}
}
