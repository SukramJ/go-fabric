// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// pushedReport encodes a ReportData the peer pushes for subscription id
// (none when id is 0), with OnOff = value when withData.
func pushedReport(t *testing.T, id uint32, withData, more, suppress, value bool) []byte {
	t.Helper()
	rd := im.ReportData{SubscriptionID: id, HasSubscription: id != 0, MoreChunkedMessages: more, SuppressResponse: suppress}
	if withData {
		rd.Reports = []im.AttributeReport{{Path: onOff, Value: im.AttributeValue{Value: value}, DataVersion: 3}}
	}
	return encode(t, func(e *tlv.Encoder) {
		rd.MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) { e.PutBool(tag, v.Value.(bool)) })
	})
}

// push opens exchange xid with the peer's first message.
func (s *scripted) push(t *testing.T, xid uint16, opcode uint8, payload []byte) {
	t.Helper()
	s.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: opcode, ExchangeID: xid, ProtocolID: im.InteractionModelProtocolID}, payload)
}

// ackPushed acknowledges to, a message we sent on an exchange the peer opened.
func (s *scripted) ackPushed(t *testing.T, to msg) {
	t.Helper()
	s.send(t, message.ProtocolHeader{Initiator: true, HasAck: true, AckCounter: to.hdr.MessageCounter, Opcode: 0x10, ExchangeID: to.proto.ExchangeID}, nil)
}

// continueOn sends the peer's next message on its exchange, acknowledging to.
func (s *scripted) continueOn(t *testing.T, to msg, opcode uint8, payload []byte) {
	t.Helper()
	s.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, HasAck: true, AckCounter: to.hdr.MessageCounter, Opcode: opcode, ExchangeID: to.proto.ExchangeID, ProtocolID: im.InteractionModelProtocolID}, payload)
}

func handlerRig(t *testing.T) (*scripted, *interaction.Subscriptions, *interaction.Interaction, chan *exchange.Session) {
	t.Helper()
	s := newScripted(t)
	started := make(chan *exchange.Session, 8)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock, ReportStarted: func(x *exchange.Session) { started <- x }})
	reg.Register(s.mgr)
	t.Cleanup(func() { s.mgr.SetProtocolHandler(im.InteractionModelProtocolID, nil) })
	return s, reg, interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg}), started
}

func expectStatus(t *testing.T, s *scripted, want im.StatusCode) msg {
	t.Helper()
	m := s.next(t)
	if m.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, m.payload) != want {
		t.Fatalf("answer opcode 0x%02X, want StatusResponse %v", m.proto.Opcode, want)
	}
	return m
}

// TestInboundReports: a report the peer pushes on its subscription in two
// chunks — each acknowledged, ReportStarted with the session, Updated with
// the merged report (ClientSubscriptionHandler.ts:106-138); an empty
// report is a keepalive (:114-128), its suppressed response honoured; the
// liveness deadline restarts after each (:139-144).
func TestInboundReports(t *testing.T) {
	t.Parallel()
	s, _, c, started := handlerRig(t)
	updates := make(chan *interaction.Report, 4)
	keepalives := make(chan struct{}, 4)
	sub := subscribed(t, s, c, interaction.SubscribeRequest{
		Attributes:        []im.ConcreteAttributePath{onOff},
		Updated:           func(r *interaction.Report) { updates <- r },
		KeepaliveReceived: func() { keepalives <- struct{}{} },
	}, 5)
	<-updates // the priming report

	s.push(t, 0x7001, im.OpcodeReportData, pushedReport(t, 5, true, true, false, true))
	first := expectStatus(t, s, im.StatusSuccess)
	if got := <-started; got != s.session {
		t.Fatal("ReportStarted with another session")
	}
	s.continueOn(t, first, im.OpcodeReportData, pushedReport(t, 5, false, false, false, false))
	final := expectStatus(t, s, im.StatusSuccess)
	s.ackPushed(t, final)
	r := <-updates
	if v, ok := r.Value(1, 6, 0); !ok || !v.El.Bool {
		t.Fatalf("pushed report %+v", r)
	}
	// The report carries its subscription id (the first chunk's) and the
	// session it arrived over.
	if r.SubscriptionID != 5 || r.Session != s.session {
		t.Fatalf("pushed report on subscription %d over session %p, want 5 over %p", r.SubscriptionID, r.Session, s.session)
	}

	s.push(t, 0x7002, im.OpcodeReportData, pushedReport(t, 5, false, false, true, false))
	select {
	case <-keepalives:
	case <-time.After(5 * time.Second):
		t.Fatal("no keepalive")
	}
	timeout := 30*time.Second + 2*c.MaximumPeerResponseTime(interaction.SubscriptionProcessingTime, false)
	s.clock.waitTimer(t, timeout)
	if len(updates) != 0 || sub.ID() != 5 {
		t.Fatal("a keepalive reached Updated")
	}
}

// TestInboundReportsRefused: the answers of ClientSubscriptionHandler.ts
// to a report it cannot take — no subscription id (:76-86), an unknown id
// (:88-104), a blocked registry (:33-43), a later chunk of another
// subscription (:174-192) — and InvalidAction for a first message that is
// not a report (InteractionMessenger.ts:357-361).
func TestInboundReportsRefused(t *testing.T) {
	t.Parallel()
	s, reg, c, _ := handlerRig(t)
	subscribed(t, s, c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}}, 5)

	s.push(t, 0x7101, im.OpcodeReportData, pushedReport(t, 0, true, false, false, true))
	s.ackPushed(t, expectStatus(t, s, im.StatusInvalidSubscription))
	s.push(t, 0x7102, im.OpcodeReportData, pushedReport(t, 99, true, false, false, true))
	s.ackPushed(t, expectStatus(t, s, im.StatusInvalidSubscription))
	s.push(t, 0x7103, im.OpcodeReadRequest, encode(t, func(e *tlv.Encoder) { im.ReadRequest{}.MarshalTLV(e) }))
	s.ackPushed(t, expectStatus(t, s, im.StatusInvalidAction))

	s.push(t, 0x7104, im.OpcodeReportData, pushedReport(t, 5, true, true, false, true))
	first := expectStatus(t, s, im.StatusSuccess)
	s.continueOn(t, first, im.OpcodeReportData, pushedReport(t, 6, true, false, false, true))
	s.ackPushed(t, expectStatus(t, s, im.StatusInvalidSubscription))

	if err := reg.BlockNewActivity(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.push(t, 0x7105, im.OpcodeReportData, pushedReport(t, 5, true, false, false, true))
	s.ackPushed(t, expectStatus(t, s, im.StatusInvalidSubscription))
}
