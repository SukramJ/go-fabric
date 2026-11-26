// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/tlv"
)

// TestCommissioningCompleteWithoutPASESessionClosesNothingW2B2a (C4,
// pase_close.go closePASESessionsOnCommissioningComplete): a
// CommissioningComplete answered OK when no PASE session is open any more
// — or the registry lists none — closes nothing and does not fail.
func TestCommissioningCompleteWithoutPASESessionClosesNothingW2B2a(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		registry bool
	}{{name: "no session registry"}, {name: "no PASE session", registry: true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.registry {
				b.AttachSessionRegistry(operational.NewManager(nil))
			}
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("closePASESessionsOnCommissioningComplete panicked: %v", p)
				}
			}()
			b.closePASESessionsOnCommissioningComplete(context.Background(), commissioningCompleteAnswer(core.CommissioningErrorOK))
		})
	}
}

// peerSessionW2B2a is a [PeerSession] on a bridge with only its session
// table wired: an exchange opens, and a send finds no listener.
func peerSessionW2B2a(t *testing.T) *PeerSession {
	t.Helper()
	const sid = uint16(0x0E21)
	key := make([]byte, 16)
	sess, err := channel.New(channel.Config{EncryptKey: key, DecryptKey: key, PeerNodeID: 0x1234, InitialCounter: 1})
	if err != nil {
		t.Fatal(err)
	}
	b := &Bridge{}
	b.sessions = lostPeerLookup{sid: sid, sess: sess, fabric: 1}
	b.sessionRegistry = &orderedRegistry{count: 1}
	return &PeerSession{b: b, link: peerLink{sessionID: sid, addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}}}
}

// octetsW2B2a encodes as an octet string of n bytes.
type octetsW2B2a int

func (n octetsW2B2a) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutOctets(tlv.ContextTag(0), make([]byte, n))
	_ = enc.EndContainer()
}

// invokeRequestLenW2B2a is the length of the InvokeRequest
// [PeerSession.Invoke] encodes for fields.
func invokeRequestLenW2B2a(t *testing.T, fields octetsW2B2a) int {
	t.Helper()
	enc := tlv.NewEncoder()
	im.MarshalInvokeRequestTLV(enc, im.InvokeRequest{Invokes: []im.CommandInvocation{{
		Path:   im.ConcreteCommandPath{Endpoint: 1, Cluster: 0x0029, Command: 0x00, HasEndpoint: true, HasCluster: true, HasCommand: true},
		Fields: fields,
	}}}, func(enc *tlv.Encoder, tag tlv.Tag, v any) { fields.EncodeTLV(enc, tag) })
	raw, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return len(raw)
}

// TestInvokeRefusesARequestLargerThanOneMessageW2B2a (C4,
// ota_provider_link.go PeerSession.Invoke): a request larger than one
// message of the session is [ErrPayloadTooLarge] and not sent; one of
// exactly the session's payload size is sent.
func TestInvokeRefusesARequestLargerThanOneMessageW2B2a(t *testing.T) {
	t.Parallel()
	limit := exchangeMaxPayloadSize
	// The octet string's length prefix is two bytes for every n here.
	base := octetsW2B2a(512)
	exact := base + octetsW2B2a(limit-invokeRequestLenW2B2a(t, base))
	if got := invokeRequestLenW2B2a(t, exact); got != limit {
		t.Fatalf("test setup: request of %d bytes, want %d", got, limit)
	}
	t.Run("one byte over", func(t *testing.T) {
		t.Parallel()
		s := peerSessionW2B2a(t)
		if got := s.b.invokeMaxPayloadSize(s.link.addr, s.link.sessionID); got != limit {
			t.Fatalf("test setup: the session carries %d, want %d", got, limit)
		}
		_, err := s.Invoke(context.Background(), 1, 0x0029, 0x00, exact+1)
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Fatalf("Invoke = %v, want ErrPayloadTooLarge", err)
		}
	})
	t.Run("exactly the limit", func(t *testing.T) {
		t.Parallel()
		s := peerSessionW2B2a(t)
		// Sent: the send is what fails, for want of a listener.
		_, err := s.Invoke(context.Background(), 1, 0x0029, 0x00, exact)
		if !errors.Is(err, ErrUnsolicitedListenerMissing) {
			t.Fatalf("Invoke = %v, want the send's ErrUnsolicitedListenerMissing", err)
		}
	})
}

// TestReceiveTimeoutOnAClosedExchangeDeclaresNothingW2B2a (C4,
// ota_provider_link.go receiveTimedOut): a receive timeout reported for an
// exchange already closed and dropped declares no peer loss, while the
// same report for the open exchange does. Deterministic: receiveTimedOut is
// called directly, where a Receive on a closed exchange with an expired
// deadline reaches it only when the select picks the deadline.
func TestReceiveTimeoutOnAClosedExchangeDeclaresNothingW2B2a(t *testing.T) {
	t.Parallel()
	lost := func(reg *orderedRegistry) bool { return len(reg.before)+len(reg.perPeer)+len(reg.closed) > 0 }
	for _, closed := range []bool{true, false} {
		s := peerSessionW2B2a(t)
		reg, _ := s.b.sessionRegistry.(*orderedRegistry)
		ex, err := s.b.openInitiatedExchange(s.link)
		if err != nil {
			t.Fatal(err)
		}
		if closed {
			_ = ex.Close()
		}
		s.b.receiveTimedOut(ex)
		if got := lost(reg); got == closed {
			t.Fatalf("closed=%v: peer declared lost = %v, want %v", closed, got, !closed)
		}
	}
}
