// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestCASEInitiatorReceivesTheResponderMessageBeforeSuccess: the
// responder of a full handshake holds its session from before its Success
// and may send on it at once; the initiator reads that message on its
// listener goroutine while its handshake goroutine may still wait for the
// Success. The initiator registers its session before Sigma3 goes out and
// holds what arrives on it until the Success activated it: the message is
// served then, not before, and not lost. Registered after the Success, it
// was dropped as one of an unknown session — for good over TCP or, as
// here, without an acknowledgement asked (review II, owner decision 269).
//
// The responder's message is put on the wire before its Success, so the
// order does not depend on scheduling.
func TestCASEInitiatorReceivesTheResponderMessageBeforeSuccess(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	peer := exchange.PeerAddress{FabricIndex: r.nodeFabric.Identity.FabricIndex, NodeID: controllerNodeID}
	served := make(chan bool, 1)
	r.nodeMgr.SetProtocolHandler(im.InteractionModelProtocolID, func(x *exchange.Exchange, _ *exchange.Message) {
		// Served only once the session is the caller's: SessionsFor
		// lists an activated session, never one still establishing.
		served <- slices.Contains(r.nodeMgr.SessionsFor(peer), x.Session())
	})

	var (
		once       sync.Once
		responder  uint16
		earlyOnSec = make(chan struct{}, 1)
	)
	r.serverRec.setHook(func(d []byte) {
		hdr, _, err := message.UnmarshalHeader(d)
		if err != nil {
			return
		}
		if hdr.SessionID != 0 {
			select {
			case earlyOnSec <- struct{}{}:
			default:
			}
			return
		}
		opcode, payload, ok := unsecuredMessage(d)
		if !ok {
			return
		}
		switch opcode {
		case mrp.SCOpcodeSigma2:
			s2, err := sigma.UnmarshalSigma2(payload)
			if err != nil {
				t.Errorf("Sigma2: %v", err)
				return
			}
			responder = s2.ResponderSessionID
		case mrp.SCOpcodeStatusReport:
			sr, ok := exchange.DecodeStatusReport(payload)
			if !ok || sr.ProtocolCode != mrp.SCStatusProtocolSessionEstablishmentSuccess {
				return
			}
			once.Do(func() {
				s := r.serverMgr.Session(responder)
				if s == nil {
					t.Errorf("responder session %d not registered at its Success", responder)
					return
				}
				go func() {
					x, err := r.serverMgr.Initiate(s, im.InteractionModelProtocolID, exchange.Options{})
					if err != nil {
						t.Errorf("initiate: %v", err)
						return
					}
					defer x.Close()
					if err := x.Send(ctx, im.OpcodeStatusResponse, []byte{0x15, 0x24, 0x00, 0x00, 0x18}, exchange.SendOptions{SuppressAck: true}); err != nil {
						t.Errorf("early send: %v", err)
					}
				}()
				wait, stop := context.WithTimeout(ctx, 5*time.Second)
				defer stop()
				select {
				case <-earlyOnSec:
				case <-wait.Done():
					t.Error("the responder's early message did not reach the socket")
				}
			})
		}
	})

	client, resumed, err := handshake.CASE(ctx, r.nodeMgr, r.serverAddr, r.nodeFabric, controllerNodeID, handshake.CASEOptions{Resumption: r.nodeStore})
	if err != nil {
		t.Fatalf("CASE: %v", err)
	}
	if resumed {
		t.Fatal("resumed, want a full handshake")
	}
	r.accepted(t)
	wait, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	select {
	case activated := <-served:
		if !activated {
			t.Fatal("the responder's message was served on a session still establishing")
		}
	case <-wait.Done():
		t.Fatalf("the responder's message on session %d, sent before its Success, never reached the initiator's handler", client.ID())
	}
}

// TestCASEInitiatorClosesTheEstablishingSessionWithoutSuccess: the
// session registered before Sigma3 is closed when the responder's Success
// does not arrive — here its Success cannot be sent — and the caller gets
// none (owner decision 269).
func TestCASEInitiatorClosesTheEstablishingSessionWithoutSuccess(t *testing.T) {
	t.Parallel()
	r := newResponderRig(t)
	ids := make(chan uint16, 1)
	r.nodeRec.setHook(func(d []byte) {
		if opcode, payload, ok := unsecuredMessage(d); ok && opcode == mrp.SCOpcodeSigma1 {
			if s1, err := sigma.UnmarshalSigma1(payload); err == nil {
				select {
				case ids <- s1.InitiatorSessionID:
				default:
				}
			}
		}
	})
	r.serverRec.setRefuse(func(d []byte) error {
		if opcode, payload, ok := unsecuredMessage(d); ok && opcode == mrp.SCOpcodeStatusReport {
			if sr, ok := exchange.DecodeStatusReport(payload); ok && sr.ProtocolCode == mrp.SCStatusProtocolSessionEstablishmentSuccess {
				return errRefused
			}
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, _, err := r.open(ctx, r.nodeFabric)
	if err == nil {
		t.Fatalf("CASE succeeded without the responder's Success (session %d)", s.ID())
	}
	var id uint16
	select {
	case id = <-ids:
	default:
		t.Fatal("no Sigma1 sent")
	}
	if got := r.nodeMgr.Session(id); got != nil {
		t.Fatalf("session %d still registered after the handshake failed (closed %v)", id, got.IsClosed())
	}
}

var errRefused = errors.New("refused")
