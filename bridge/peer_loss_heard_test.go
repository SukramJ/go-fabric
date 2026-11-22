// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/sessionseam"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// heardRig is a device with one CASE session it opened toward a peer that
// never answers on its own, and an exchange this node opened on it with
// one reliable message sent. The test speaks for the peer through peer.
type heardRig struct {
	dev      *resumeDevice
	sid      uint16
	peer     *channel.Session
	peerAddr *net.UDPAddr
	ex       *initiatedSecureExchange
}

func newHeardRig(t *testing.T) *heardRig {
	t.Helper()
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, _ *operational.Manager) {
		br.AttachAckTracker(mrp.NewAckTracker(0))
	})
	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	peerAddr, ok := silent.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address %T", silent.LocalAddr())
	}
	const sid = uint16(0x0E01)
	keys := peerLossKeys(t)
	// The device initiated the session: it seals with I2R, the peer with R2I.
	if _, err := sessionseam.OpenAt(dev.sessions, sid, true, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0F01, nil, keys); err != nil {
		t.Fatal(err)
	}
	peer, err := channel.New(channel.Config{
		EncryptKey: keys.R2IKey[:], DecryptKey: keys.I2RKey[:],
		LocalNodeID: resumeCtrlNode, PeerNodeID: resumeDeviceNode, PeerSessionID: sid,
	})
	if err != nil {
		t.Fatal(err)
	}
	dev.br.sessionPeerAddrs.Store(sid, peerAddr)
	link, ok := dev.br.existingPeerSession(resumeFabricIndex, resumeCtrlNode)
	if !ok {
		t.Fatal("no session to open the exchange on")
	}
	ex, err := dev.br.openInitiatedExchange(link)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ex.Close() })
	if ex.sessionID != sid {
		t.Fatalf("exchange opened on session %#x, want %#x", ex.sessionID, sid)
	}
	if err := ex.Send(context.Background(), im.InteractionModelProtocolID, im.OpcodeInvokeRequest, []byte{0x15, 0x18}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	return &heardRig{dev: dev, sid: sid, peer: peer, peerAddr: peerAddr, ex: ex}
}

// lastSentCounter is the message counter of the exchange's last reliable
// message.
func (r *heardRig) lastSentCounter(t *testing.T) uint32 {
	t.Helper()
	n := r.ex.lastTracked.Load()
	if n == 0 {
		t.Fatal("the exchange tracked no reliable message")
	}
	return uint32(n - 1)
}

// standaloneAck has the peer acknowledge counter on the exchange through
// the bridge's receive path.
func (r *heardRig) standaloneAck(t *testing.T, counter uint32) {
	t.Helper()
	proto := message.ProtocolHeader{
		HasAck: true, AckCounter: counter, Opcode: mrp.StandaloneAckOpcode,
		ExchangeID: r.ex.exchangeID, ProtocolID: mrp.SecureChannelProtocolID,
	}
	hdr := message.Header{SessionID: r.sid}
	sealed, err := r.peer.Encrypt(&hdr, securityFlagsByte(&hdr), proto.Marshal())
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	r.dev.br.handleDatagram(append(hdr.Marshal(), sealed.Ciphertext...), r.peerAddr)
}

// giveUp drives the exchange's outstanding message to exhaustion without
// waiting out the backoff: each tick is an hour later than the previous one.
func (r *heardRig) giveUp(t *testing.T) {
	t.Helper()
	r.dev.br.mu.RLock()
	outbound := r.dev.br.outboundReliable
	r.dev.br.mu.RUnlock()
	if outbound == nil {
		t.Fatal("no outbound-reliable tracker")
	}
	now := time.Now()
	for i := 0; i <= mrp.MaxRetransmissions+1; i++ {
		r.dev.br.tickOutboundReliable(outbound, now.Add(time.Duration(i+1)*time.Hour))
	}
}

// requireFailed fails t unless the exchange ended on the exhaustion.
func (r *heardRig) requireFailed(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := r.ex.Receive(ctx); !errors.Is(err, mrp.ErrMaxRetransmissionsReached) {
		t.Errorf("Receive after the exhaustion: %v, want ErrMaxRetransmissionsReached", err)
	}
}

// TestInitiatedExchangePeerLossDeclarationParity ports matter.js
// packages/protocol/test/protocol/MessageExchangeTest.ts:106-124 at
// b25ed960 ("peer loss declaration" / "send()") by meaning onto an exchange
// this node opened (the OTA requestor's, ota_provider_link.go): a message
// given up reports the peer lost — every session with it force-closed —
// only when the exchange never heard from the peer. Mirrors matter.js
// MessageExchange.ts:674-681 at b25ed960 (#reportsPeerLoss: only while
// #messageReceivedCounter === 0) and :490 (the counter counts every message
// handed to the exchange, a standalone acknowledgement as much as any).
func TestInitiatedExchangePeerLossDeclarationParity(t *testing.T) {
	t.Parallel()
	t.Run("send()/declares peer lost when exchange has received no messages", func(t *testing.T) {
		t.Parallel()
		r := newHeardRig(t)
		r.giveUp(t)
		if _, err := r.dev.sessions.Get(r.sid); !errors.Is(err, operational.ErrSessionNotFound) {
			t.Errorf("session %#x after a give-up on an exchange that heard nothing: %v, want ErrSessionNotFound", r.sid, err)
		}
		r.requireFailed(t)
	})
	t.Run("send()/does not declare peer lost when exchange already received at least one message", func(t *testing.T) {
		t.Parallel()
		r := newHeardRig(t)
		// The peer acknowledges the first message: the exchange heard from it.
		r.standaloneAck(t, r.lastSentCounter(t))
		r.dev.br.mu.RLock()
		outbound := r.dev.br.outboundReliable
		r.dev.br.mu.RUnlock()
		if c, pending := outbound.pendingOnExchange(r.sid, r.ex.exchangeID, exchangeInitiator); pending {
			t.Fatalf("message %d still waits for its acknowledgement after the peer's standalone ack", c)
		}
		if err := r.ex.Send(context.Background(), im.InteractionModelProtocolID, im.OpcodeInvokeRequest, []byte{0x15, 0x18}); err != nil {
			t.Fatalf("second Send: %v", err)
		}
		r.giveUp(t)
		if _, err := r.dev.sessions.Get(r.sid); err != nil {
			t.Errorf("session %#x closed by a give-up on an exchange the peer answered: %v", r.sid, err)
		}
		r.requireFailed(t)
	})
}
