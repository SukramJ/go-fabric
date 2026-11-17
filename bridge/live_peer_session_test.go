// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/internal/sessionseam"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// TestLivePeerSessionNewestByPeerActivity: of two live sessions with the
// peer, the one the peer sent on last is reused, whatever its id — not the
// highest id (review II, F-db021da0-34, R2-P5-port-07; matter.js
// newestSession, packages/protocol/src/peer/Peer.ts:582-602 at db021da0).
// A Large Message requirement takes only a session bound to a TCP
// connection (R2-P5-peer-03), and finding none starts no handshake.
func TestLivePeerSessionNewestByPeerActivity(t *testing.T) {
	t.Parallel()
	stamp := &stampClock{}
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		onStampClock(br, sessions, stamp)
	})
	const (
		olderSID = uint16(0x0B01)
		newerSID = uint16(0x0B02)
	)
	olderEntry, err := sessionseam.OpenAt(dev.sessions, olderSID, false, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0C01, nil, peerLossKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	older, ok := olderEntry.(*operational.Entry)
	if !ok {
		t.Fatalf("OpenAt installed %T", olderEntry)
	}
	stamp.step(time.Millisecond)
	if _, err := sessionseam.OpenAt(dev.sessions, newerSID, false, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0C02, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}
	udp := &net.UDPAddr{IP: net.IPv6loopback, Port: 5541}
	dev.br.sessionPeerAddrs.Store(olderSID, udp)
	dev.br.sessionPeerAddrs.Store(newerSID, udp)

	// The peer sends on the session with the lower id.
	stamp.step(time.Millisecond)
	older.MarkActiveRx()
	link, ok := dev.br.existingPeerSession(resumeFabricIndex, resumeCtrlNode)
	if !ok || link.sessionID != olderSID {
		t.Fatalf("reused session %#x (found %v), want %#x — the one the peer sent on last", link.sessionID, ok, olderSID)
	}
	s, ok := dev.br.LivePeerSession(resumeFabricIndex, resumeCtrlNode, PeerSessionRequirement{})
	if !ok || s.link.sessionID != olderSID {
		t.Fatalf("LivePeerSession = %+v, %v; want %#x", s, ok, olderSID)
	}
	if s.transport() != "udp" {
		t.Errorf("Transport = %q, want udp", s.transport())
	}
	got, ok := s.LastActivityMonotonic()
	want, _ := older.LastPeerActivityMonotonic()
	if !ok || got != want {
		t.Errorf("LastActivityMonotonic = %v, %v; want the session's %v", got, ok, want)
	}

	// A Large Message requirement: no TCP session, none — and no handshake
	// (no initiator is attached; LivePeerSession never reaches for one).
	if _, ok := dev.br.LivePeerSession(resumeFabricIndex, resumeCtrlNode, PeerSessionRequirement{LargeMessage: true}); ok {
		t.Fatal("a UDP session was offered for a Large Message request")
	}
	// The newer-id session moves onto a TCP connection: it is the one a
	// Large Message request takes, although the peer sent on the other
	// one last.
	dev.br.sessionPeerAddrs.Store(newerSID, dev.br.routeFor(&fakeConn{}))
	s, ok = dev.br.LivePeerSession(resumeFabricIndex, resumeCtrlNode, PeerSessionRequirement{LargeMessage: true})
	if !ok || s.link.sessionID != newerSID || s.transport() != "tcp" {
		t.Fatalf("Large Message session = %+v, %v; want %#x over tcp", s, ok, newerSID)
	}
	if _, ok := dev.br.LivePeerSession(resumeFabricIndex+1, resumeCtrlNode, PeerSessionRequirement{}); ok {
		t.Error("a session of another fabric was offered")
	}
}
