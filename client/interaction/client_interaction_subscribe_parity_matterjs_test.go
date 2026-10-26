// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// The ClientInteraction.close and abandoned-subscribe cases of matter.js's
// ClientInteractionSubscribeTest (packages/protocol/test/action/client/ClientInteractionSubscribeTest.ts
// at 862ad75f) on the scripted peer: Close (ClientInteraction.ts:264-297),
// the in-flight set it drains, and the abandoned establishment
// (:1084-1097, InteractionMessenger.ts:1116-1137).

// protoOn is a reliable message of the peer on to's exchange.
func protoOn(to msg, opcode uint8) message.ProtocolHeader {
	return message.ProtocolHeader{NeedsAck: true, Opcode: opcode, ExchangeID: to.proto.ExchangeID, ProtocolID: im.InteractionModelProtocolID}
}

// TestClientInteractionSubscribeAbortedParity: an aborted subscribe
// returns at once; its exchange stays open for the peer's report until
// the interaction closes, and Close ends that wait (:187-201).
func TestClientInteractionSubscribeAbortedParity(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := subscribeInteraction(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.Subscribe(ctx, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}})
		done <- err
	}()
	req := s.next(t)
	if req.proto.Opcode != im.OpcodeSubscribeRequest {
		t.Fatalf("opcode 0x%02X", req.proto.Opcode)
	}
	s.ack(t, req)
	cancel()
	if err := await(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("subscribe: %v, want the abort", err)
	}
	closed := make(chan struct{})
	go func() {
		c.Close(nil)
		close(closed)
	}()
	await(t, closed)
	// Closed: a new interaction is refused with the close reason.
	if _, err := c.Read(context.Background(), onOff); !errors.Is(err, interaction.ErrClosed) {
		t.Fatalf("read after Close: %v, want ErrClosed", err)
	}
}

// TestClientInteractionSubscribeAbandonedReportParity: a priming report
// that arrives after the abort is answered INVALID_SUBSCRIPTION — the
// abandoned establishment awaits it (InteractionMessenger.ts:1116-1129).
func TestClientInteractionSubscribeAbandonedReportParity(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := subscribeInteraction(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.Subscribe(ctx, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}})
		done <- err
	}()
	req := s.next(t)
	s.ack(t, req)
	cancel()
	if err := await(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("subscribe: %v, want the abort", err)
	}
	s.send(t, protoOn(req, im.OpcodeReportData), reportChunk(t, false, false,
		data(onOff, func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, true) })))
	status := s.next(t)
	if status.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, status.payload) != im.StatusInvalidSubscription {
		t.Fatalf("answer opcode 0x%02X, want StatusResponse INVALID_SUBSCRIPTION", status.proto.Opcode)
	}
	s.ack(t, status)
	c.Close(nil)
}

// TestClientInteractionSubscribeClosedWhileProcessingParity: a report
// being processed when the interaction closes is answered
// INVALID_SUBSCRIPTION once, without retransmission, and Close returns
// (:256-292).
func TestClientInteractionSubscribeClosedWhileProcessingParity(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := subscribeInteraction(s)
	processing, proceed := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOff},
			Updated: func(*interaction.Report) {
				close(processing)
				<-proceed
			},
		})
		done <- err
	}()
	req := s.next(t)
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, false,
		data(onOff, func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, true) })))
	<-processing
	closed := make(chan struct{})
	go func() {
		c.Close(nil)
		close(closed)
	}()
	// Close aborts the establishment while the report is still owed: it
	// refuses new work first and aborts right after.
	// The probe's context is done, so a read before the close sends
	// nothing.
	probe, stop := context.WithCancel(context.Background())
	stop()
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := c.Read(probe, onOff); errors.Is(err, interaction.ErrClosed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Close did not take effect")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond)
	close(proceed)
	if err := await(t, done); !errors.Is(err, interaction.ErrClosed) {
		t.Fatalf("subscribe: %v, want the close reason", err)
	}
	status := s.next(t)
	if status.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, status.payload) != im.StatusInvalidSubscription {
		t.Fatalf("answer opcode 0x%02X, want StatusResponse INVALID_SUBSCRIPTION", status.proto.Opcode)
	}
	await(t, closed)
	select {
	case b := <-s.conn.ch:
		t.Fatalf("a retransmission after Close (%d bytes)", len(b))
	case <-time.After(50 * time.Millisecond):
	}
}
