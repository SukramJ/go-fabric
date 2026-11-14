// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"bytes"
	"errors"
	"testing"
)

// oneDestination resolves the one destination id it holds to ours and
// misses every other.
type oneDestination struct {
	dest [32]byte
	ours *Identity
}

func (o oneDestination) ResolveSigma1Destination(dest [32]byte, _ [RandomSize]byte) (*Identity, PeerVerifier, bool) {
	if dest != o.dest {
		return nil, nil, false
	}
	return o.ours, testVerifier{}, true
}

// refusedSigma1s are Sigma1s that decode and begin no handshake on a
// responder that resolves only the destination of the handshake under
// test, whose destination id is dest: one addressing no fabric of the
// node, and one addressing dest whose ephemeral key is no point.
func refusedSigma1s(t *testing.T, ipk [16]byte, dest [32]byte) map[string]struct {
	payload []byte
	want    error
} {
	t.Helper()
	other := NewInitiator(newTestIdentity(t, 0xCCCC, 0x77, ipk), testVerifier{}, 0x1003, [32]byte{0xCC})
	foreign, err := other.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	badEph := make([]byte, EphPubKeySize)
	badEph[0] = 0x05
	offCurve := Sigma1{InitiatorSessionID: 7, DestinationID: dest, InitiatorEphPubKey: badEph}.Marshal()
	return map[string]struct {
		payload []byte
		want    error
	}{
		"addressing no fabric of the node":    {foreign, ErrNoSharedTrustRoots},
		"with an ephemeral key off the curve": {offCurve, ErrInvalidPoint},
	}
}

// TestRefusedSigma1LeavesTheResponderAsItWas: a Sigma1 that decodes and
// is refused — no fabric for its destination id, an ephemeral key that is
// no point — changes nothing of the responder it reaches, through either
// entry point: the handshake in progress keeps its Sigma2, its keys and
// its session id, takes no id from the renewer, and its initiator's Sigma3
// completes it; a finished handshake keeps its session keys and its state,
// and its Sigma3 retransmission is still accepted (review II,
// R2-P4-fr-sec-03). matter.js changes nothing about the handshake in
// progress: the message never reaches its CASE server on the live exchange
// (packages/protocol/src/protocol/MessageExchange.ts:521-534 at db021da0).
func TestRefusedSigma1LeavesTheResponderAsItWas(t *testing.T) {
	t.Parallel()
	ipk := fabricIPK()
	const fabricID = uint64(0x2A)
	for _, withResume := range []bool{false, true} {
		process := func(r *Responder, payload []byte) error {
			if withResume {
				_, err := r.ProcessSigma1WithResume(payload)
				return err
			}
			_, err := r.ProcessSigma1(payload)
			return err
		}
		begin := func(t *testing.T) (*Responder, *Initiator, Sigma2, []byte, *int, [32]byte) {
			t.Helper()
			ours := newTestIdentity(t, 0xBBBB, fabricID, ipk)
			initiator := NewInitiator(newTestIdentity(t, 0xAAAA, fabricID, ipk), testVerifier{}, 0x1001, [32]byte{0xAA})
			s1, err := initiator.GenerateSigma1()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := UnmarshalSigma1(s1)
			if err != nil {
				t.Fatal(err)
			}
			responder := NewResponder(ours, testVerifier{}, 0x2001)
			responder.SetIdentityResolver(oneDestination{dest: decoded.DestinationID, ours: ours})
			renewed := new(int)
			responder.SetSessionIDRenewer(func(previous uint16) (uint16, bool) {
				*renewed++
				return previous + 0x10, true
			})
			s2, err := responder.ProcessSigma1(s1)
			if err != nil {
				t.Fatalf("ProcessSigma1: %v", err)
			}
			return responder, initiator, s2, s1, renewed, decoded.DestinationID
		}
		for name := range refusedSigma1s(t, ipk, [32]byte{}) {
			t.Run(name+map[bool]string{false: "", true: ", with resumption"}[withResume]+", handshake in progress", func(t *testing.T) {
				t.Parallel()
				responder, initiator, s2, s1, renewed, dest := begin(t)
				refused := refusedSigma1s(t, ipk, dest)[name]
				calls, sessionID := *renewed, responder.SessionID()
				if err := process(responder, refused.payload); !errors.Is(err, refused.want) {
					t.Fatalf("the refused Sigma1: %v, want %v", err, refused.want)
				}
				if *renewed != calls || responder.SessionID() != sessionID {
					t.Fatalf("the refused Sigma1 moved the session id %#x → %#x (renewer calls %d → %d)", sessionID, responder.SessionID(), calls, *renewed)
				}
				again, err := responder.ProcessSigma1(s1)
				if err != nil || !bytes.Equal(again.Marshal(), s2.Marshal()) {
					t.Fatalf("the handshake's Sigma1 again after the refused one: %v, want the cached Sigma2", err)
				}
				s3, err := initiator.ProcessSigma2(s2)
				if err != nil {
					t.Fatal(err)
				}
				if err := responder.ProcessSigma3(s3); err != nil {
					t.Fatalf("the handshake's Sigma3 after the refused Sigma1: %v, want it completed", err)
				}
				ik, _ := initiator.SessionKeys()
				rk, ok := responder.SessionKeys()
				if !ok || !constantTimeKeysEqual(ik, rk) {
					t.Fatal("the keys of the completed handshake do not match the initiator's")
				}
			})
			t.Run(name+map[bool]string{false: "", true: ", with resumption"}[withResume]+", handshake finished", func(t *testing.T) {
				t.Parallel()
				responder, initiator, s2, _, renewed, dest := begin(t)
				refused := refusedSigma1s(t, ipk, dest)[name]
				s3, err := initiator.ProcessSigma2(s2)
				if err != nil {
					t.Fatal(err)
				}
				if err := responder.ProcessSigma3(s3); err != nil {
					t.Fatal(err)
				}
				keys, _ := responder.SessionKeys()
				calls, sessionID, peer := *renewed, responder.SessionID(), responder.PeerNodeID()
				if err := process(responder, refused.payload); !errors.Is(err, refused.want) {
					t.Fatalf("the refused Sigma1: %v, want %v", err, refused.want)
				}
				got, ok := responder.SessionKeys()
				if !ok || !constantTimeKeysEqual(got, keys) {
					t.Fatal("the refused Sigma1 took the finished handshake's session keys")
				}
				if *renewed != calls || responder.SessionID() != sessionID || responder.PeerNodeID() != peer {
					t.Fatalf("the refused Sigma1 changed the finished handshake: session id %#x → %#x, peer %#x → %#x, renewer calls %d → %d",
						sessionID, responder.SessionID(), peer, responder.PeerNodeID(), calls, *renewed)
				}
				if err := responder.ProcessSigma3(s3); err != nil {
					t.Fatalf("the finished handshake's Sigma3 again after the refused Sigma1: %v, want accepted", err)
				}
			})
		}
	}
}

// TestFinishedResponderAcceptsOnlyItsOwnSigma3: once a handshake finished,
// ProcessSigma3 accepts the bytes of the Sigma3 that finished it — its
// retransmission — and refuses any other with [ErrSessionState], changing
// nothing: the state, the session keys and the peer stay (review II,
// phase 4 fr-sec). A handshake finished by resumption had no Sigma3, so
// every Sigma3 is refused.
func TestFinishedResponderAcceptsOnlyItsOwnSigma3(t *testing.T) {
	t.Parallel()
	ipk := fabricIPK()
	responder := NewResponder(newTestIdentity(t, 0xBBBB, 0x2A, ipk), testVerifier{}, 0x2001)
	initiator := NewInitiator(newTestIdentity(t, 0xAAAA, 0x2A, ipk), testVerifier{}, 0x1001, [32]byte{0xAA})
	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := responder.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	s3, err := initiator.ProcessSigma2(s2)
	if err != nil {
		t.Fatal(err)
	}
	if err := responder.ProcessSigma3(s3); err != nil {
		t.Fatal(err)
	}
	keys, _ := responder.SessionKeys()
	peer := responder.PeerNodeID()
	other := append([]byte(nil), s3...)
	other[len(other)-1] ^= 0xFF
	for _, payload := range [][]byte{other, {0x15, 0x18}, nil} {
		if err := responder.ProcessSigma3(payload); !errors.Is(err, ErrSessionState) {
			t.Fatalf("a Sigma3 of other bytes on the finished responder: %v, want ErrSessionState", err)
		}
	}
	if got, ok := responder.SessionKeys(); !ok || !constantTimeKeysEqual(got, keys) || responder.PeerNodeID() != peer {
		t.Fatal("a refused Sigma3 changed the finished handshake")
	}
	if err := responder.ProcessSigma3(s3); err != nil {
		t.Fatalf("the finishing Sigma3 again: %v, want accepted", err)
	}
}
