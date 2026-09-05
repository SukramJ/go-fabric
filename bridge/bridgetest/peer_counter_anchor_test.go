// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// The two node ids the paired sessions below build their AES-CCM nonces
// from, and the session id the bridge resolves the receiving half under.
const (
	anchorBridgeNodeID uint64 = 0x0000000000000001
	anchorPeerNodeID   uint64 = 0x00000000DEADBEEF
	anchorSessionID    uint16 = 0x1234
)

// anchorFirstCounter is the counter the modelled SubscribeRequest carries.
// The peer's next two messages are anchorFirstCounter+1 and +2 — the
// StandaloneAck and the StatusResponse that follow a report, which is the
// pair that races.
const anchorFirstCounter uint32 = 200

// newPairedSessions returns the bridge's half of a secure session and the
// peer's mirror of it: same two keys, swapped directions, so a frame the
// peer encrypts is one the bridge half decrypts.
//
// The peer's outbound counter is primed one past the modelled
// SubscribeRequest, so its first two Encrypt calls consume
// anchorFirstCounter+1 and anchorFirstCounter+2.
func newPairedSessions(t *testing.T) (bridgeSide, peerSide *channel.Session) {
	t.Helper()

	i2r := []byte("0123456789abcdef")
	r2i := []byte("fedcba9876543210")

	bridgeSide, err := channel.New(channel.Config{
		EncryptKey:  r2i,
		DecryptKey:  i2r,
		LocalNodeID: anchorBridgeNodeID,
		PeerNodeID:  anchorPeerNodeID,
	})
	if err != nil {
		t.Fatalf("bridge-side channel.New: %v", err)
	}
	peerSide, err = channel.New(channel.Config{
		EncryptKey:     i2r,
		DecryptKey:     r2i,
		LocalNodeID:    anchorPeerNodeID,
		PeerNodeID:     anchorBridgeNodeID,
		InitialCounter: anchorFirstCounter + 1,
	})
	if err != nil {
		t.Fatalf("peer-side channel.New: %v", err)
	}
	return bridgeSide, peerSide
}

// sealed is one frame the peer produced: the header it authenticated under
// and the ciphertext that binds to it.
type sealed struct {
	hdr *message.Header
	ct  []byte
}

// sealNext has the peer encrypt one frame under its next outbound counter.
// Each frame gets its own header because the AAD is the marshalled header,
// which carries the counter.
func sealNext(t *testing.T, peerSide *channel.Session, payload []byte) sealed {
	t.Helper()

	// HasSourceNodeID stays false: secure unicast omits the source node id
	// and the receiver derives the nonce node id from the session context
	// (secure/channel/session.go, Encrypt).
	hdr := &message.Header{SessionID: anchorSessionID}
	res, err := peerSide.Encrypt(hdr, 0, payload)
	if err != nil {
		t.Fatalf("peer Encrypt: %v", err)
	}
	if hdr.MessageCounter != res.Counter {
		t.Fatalf("header counter %d != result counter %d", hdr.MessageCounter, res.Counter)
	}
	return sealed{hdr: hdr, ct: res.Ciphertext}
}

// attachSession wires bridgeSide into the bridge under anchorSessionID, the
// same port the receive pipeline resolves a session through.
func attachSession(b *bridge.Bridge, bridgeSide *channel.Session) {
	b.AttachSessionLookup(bridge.NewOperationalSessionLookup(
		func(id uint16) (*channel.Session, bool) {
			if id != anchorSessionID {
				return nil, false
			}
			return bridgeSide, true
		},
	))
}

// TestEstablishSubscriptionTarget_AnchorsThePeerReceiveWindow is the effect
// the PeerCounter field exists for. The peer's StandaloneAck and its
// follow-up StatusResponse are handed to the session out of order — the
// later counter first — and the earlier one must still decrypt as fresh.
//
// Without an anchor the window primes on whichever arrives first, with the
// all-ones bitmap a secure session uses (transport/mrp/window.go:72), so
// everything below it reads as already-received
// (transport/mrp/window.go:130) and the receive path drops it into the
// duplicate branch (bridge/receive.go:162) instead of processing it.
func TestEstablishSubscriptionTarget_AnchorsThePeerReceiveWindow(t *testing.T) {
	t.Parallel()

	b := newBridge(t)
	_, addr := newPeer(t)
	sub := newSubscription(t, b)
	bridgeSide, peerSide := newPairedSessions(t)
	attachSession(b, bridgeSide)

	if err := bridgetest.EstablishSubscriptionTarget(b, sub.ID, bridgetest.SubscriptionTarget{
		Peer:        addr,
		SessionID:   anchorSessionID,
		ExchangeID:  7,
		PeerCounter: anchorFirstCounter,
	}); err != nil {
		t.Fatalf("EstablishSubscriptionTarget: %v", err)
	}

	ack := sealNext(t, peerSide, []byte("standalone-ack"))
	status := sealNext(t, peerSide, []byte("status-response"))
	if ack.hdr.MessageCounter != anchorFirstCounter+1 || status.hdr.MessageCounter != anchorFirstCounter+2 {
		t.Fatalf("counters = %d, %d; want %d, %d",
			ack.hdr.MessageCounter, status.hdr.MessageCounter,
			anchorFirstCounter+1, anchorFirstCounter+2)
	}

	// Out of order on purpose: the follow-up overtakes the ack.
	if _, duplicate, err := bridgeSide.Decrypt(status.hdr, 0, status.ct); err != nil || duplicate {
		t.Fatalf("counter %d: duplicate = %v, err = %v; want false, nil",
			status.hdr.MessageCounter, duplicate, err)
	}
	_, duplicate, err := bridgeSide.Decrypt(ack.hdr, 0, ack.ct)
	if err != nil {
		t.Fatalf("counter %d: Decrypt: %v", ack.hdr.MessageCounter, err)
	}
	if duplicate {
		t.Fatalf("counter %d arriving after %d was rejected as a duplicate; the established target left the peer's receive window unanchored",
			ack.hdr.MessageCounter, status.hdr.MessageCounter)
	}
}

// TestPeerReceiveWindowIsUnanchoredWithoutAPeerCounter is the control for
// the test above: the identical out-of-order pair on a session the target
// never anchored must lose the earlier counter to the duplicate path.
// Without it, a window that accepted both for some unrelated reason would
// read as a pass.
//
// It establishes nothing, because the secure path now refuses a target with
// no PeerCounter — which is itself the state under test.
func TestPeerReceiveWindowIsUnanchoredWithoutAPeerCounter(t *testing.T) {
	t.Parallel()

	b := newBridge(t)
	_, addr := newPeer(t)
	sub := newSubscription(t, b)
	bridgeSide, peerSide := newPairedSessions(t)
	attachSession(b, bridgeSide)

	err := bridgetest.EstablishSubscriptionTarget(b, sub.ID, bridgetest.SubscriptionTarget{
		Peer:       addr,
		SessionID:  anchorSessionID,
		ExchangeID: 7,
	})
	if !errors.Is(err, bridgetest.ErrNoPeerCounter) {
		t.Fatalf("err = %v, want ErrNoPeerCounter", err)
	}

	ack := sealNext(t, peerSide, []byte("standalone-ack"))
	status := sealNext(t, peerSide, []byte("status-response"))

	if _, duplicate, err := bridgeSide.Decrypt(status.hdr, 0, status.ct); err != nil || duplicate {
		t.Fatalf("counter %d: duplicate = %v, err = %v; want false, nil",
			status.hdr.MessageCounter, duplicate, err)
	}
	_, duplicate, err := bridgeSide.Decrypt(ack.hdr, 0, ack.ct)
	if err != nil {
		t.Fatalf("counter %d: Decrypt: %v", ack.hdr.MessageCounter, err)
	}
	if !duplicate {
		t.Fatalf("counter %d arriving after %d was accepted on an unanchored window; the anchor the effect test measures is not what makes it acceptable",
			ack.hdr.MessageCounter, status.hdr.MessageCounter)
	}
}

// TestEstablishSubscriptionTarget_PeerCounterRejections covers the shapes
// where an anchor is asked for but no window can take it.
func TestEstablishSubscriptionTarget_PeerCounterRejections(t *testing.T) {
	t.Parallel()

	_, addr := newPeer(t)

	tests := []struct {
		name   string
		attach bool
		target bridgetest.SubscriptionTarget
		want   error
	}{
		{
			name:   "secure session the bridge cannot resolve",
			attach: false,
			target: bridgetest.SubscriptionTarget{Peer: addr, SessionID: anchorSessionID, PeerCounter: anchorFirstCounter},
			want:   bridgetest.ErrPeerCounterNotAnchored,
		},
		{
			name:   "unsecured target with no peer node id to key the window on",
			attach: false,
			target: bridgetest.SubscriptionTarget{Peer: addr, PeerCounter: anchorFirstCounter},
			want:   bridgetest.ErrPeerCounterNotAnchored,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newBridge(t)
			sub := newSubscription(t, b)
			if tc.attach {
				bridgeSide, _ := newPairedSessions(t)
				attachSession(b, bridgeSide)
			}
			if err := bridgetest.EstablishSubscriptionTarget(b, sub.ID, tc.target); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestEstablishSubscriptionTarget_AnchorsTheUnsecuredWindow pins the
// session-0 half of the contract: an unsecured target that names both a
// peer node id and a counter anchors the per-source window, so the same
// counter arriving afterwards is the duplicate it would be on the wire.
func TestEstablishSubscriptionTarget_AnchorsTheUnsecuredWindow(t *testing.T) {
	t.Parallel()

	b := newBridge(t)
	_, addr := newPeer(t)
	sub := newSubscription(t, b)

	target := bridgetest.SubscriptionTarget{
		Peer:          addr,
		ExchangeID:    7,
		PeerNodeID:    anchorPeerNodeID,
		HasPeerNodeID: true,
		PeerCounter:   anchorFirstCounter,
	}
	if err := bridgetest.EstablishSubscriptionTarget(b, sub.ID, target); err != nil {
		t.Fatalf("EstablishSubscriptionTarget: %v", err)
	}
	// A second establish on the same counter finds it already recorded,
	// which is the only observation of the unsecured window this package
	// can make without the wire.
	if err := bridgetest.EstablishSubscriptionTarget(b, sub.ID, target); !errors.Is(err, bridgetest.ErrPeerCounterNotAnchored) {
		t.Errorf("re-anchoring the same counter: err = %v, want ErrPeerCounterNotAnchored", err)
	}
}
