// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

type subscribeResult struct {
	sub *interaction.Subscription
	err error
}

func subscribeAsync(c *interaction.Interaction, req interaction.SubscribeRequest) <-chan subscribeResult {
	ch := make(chan subscribeResult, 1)
	go func() {
		sub, err := c.Subscribe(context.Background(), req)
		ch <- subscribeResult{sub, err}
	}()
	return ch
}

func subscribeResponse(t *testing.T, id uint32, maxInterval uint16) []byte {
	t.Helper()
	return encode(t, func(e *tlv.Encoder) { im.SubscribeResponse{SubscriptionID: id, MaxInterval: maxInterval}.MarshalTLV(e) })
}

// subscribed runs a subscription to OnOff on the scripted peer: the
// request, a one-chunk priming report, the SubscribeResponse.
func subscribed(t *testing.T, s *scripted, c *interaction.Interaction, req interaction.SubscribeRequest, id uint32) *interaction.Subscription {
	t.Helper()
	done := subscribeAsync(c, req)
	r := s.next(t)
	if r.proto.Opcode != im.OpcodeSubscribeRequest {
		t.Fatalf("opcode 0x%02X", r.proto.Opcode)
	}
	s.reply(t, r, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, false,
		data(onOff, func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, false) })))
	ack := s.next(t)
	if ack.proto.Opcode != im.OpcodeStatusResponse {
		t.Fatalf("priming acknowledgement opcode 0x%02X", ack.proto.Opcode)
	}
	s.reply(t, ack, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, id, 30))
	res := await(t, done)
	if res.err != nil {
		t.Fatal(res.err)
	}
	return res.sub
}

var onOff = im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0006, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}

// TestSubscribe: the SubscribeRequest's shape (TlvSubscribeRequest:
// keepSubscriptions, the intervals in seconds, the paths, fabric-filtered),
// the priming report in two chunks, each acknowledged, handed to Updated
// once merged (CI:1039-1048, InteractionMessenger.ts:938-972), the
// SubscribeResponse, and the subscription registered by (peer, id)
// (CI:1051-1063).
func TestSubscribe(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
	updates := make(chan *interaction.Report, 4)
	done := subscribeAsync(c, interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{onOff}, MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(time.Hour),
		Updated: func(r *interaction.Report) { updates <- r },
	})
	req := s.next(t)
	sr, err := im.UnmarshalSubscribeRequestTLV(tlv.NewDecoder(req.payload))
	if err != nil || !sr.KeepSubscriptions || sr.MinIntervalFloor != 0 || sr.MaxIntervalCeiling != 3600 || len(sr.AttributeRequests) != 1 || !sr.FabricFiltered {
		t.Fatalf("SubscribeRequest %+v (%v)", sr, err)
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, true, false,
		data(onOff, func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, true) })))
	ack := s.next(t)
	if ack.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, ack.payload) != im.StatusSuccess {
		t.Fatalf("first chunk acknowledgement 0x%02X", ack.proto.Opcode)
	}
	if len(updates) != 0 {
		t.Fatal("Updated before the report's last chunk")
	}
	level := im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0008, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	s.reply(t, ack, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, false,
		data(level, func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 7) })))
	final := s.next(t)
	if final.proto.Opcode != im.OpcodeStatusResponse {
		t.Fatalf("final acknowledgement 0x%02X", final.proto.Opcode)
	}
	s.reply(t, final, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 0x0101_0101, 30))
	res := await(t, done)
	if res.err != nil {
		t.Fatal(res.err)
	}
	sub := res.sub
	if sub.ID() != 0x0101_0101 || sub.MaxInterval() != 30*time.Second || sub.Peer() != (exchange.PeerAddress{FabricIndex: 1, NodeID: 0x2222}) {
		t.Fatalf("subscription %d, %v, %+v", sub.ID(), sub.MaxInterval(), sub.Peer())
	}
	if reg.GetPeer(sub.Peer(), sub.ID()) != sub || len(reg.Active()) != 1 {
		t.Fatal("not registered")
	}
	r := <-updates
	if v, ok := r.Value(1, 6, 0); !ok || !v.El.Bool || len(r.Attributes) != 2 {
		t.Fatalf("priming report %+v", r)
	}
	if r.Session != s.session {
		t.Fatalf("priming report over session %p, want %p", r.Session, s.session)
	}
	sub.Close()
	sub.Close()
	<-sub.Done()
	if reg.GetPeer(sub.Peer(), sub.ID()) != nil || len(reg.Active()) != 0 {
		t.Fatal("a closed subscription stays registered")
	}
}

// TestSubscriptionLiveness: the registry's one timer runs for the earliest
// deadline, the maximum interval plus twice the maximum peer response time
// of a 10 s processing time (PeerSubscription.ts:38-40, CI:1061); a
// subscription past it closes (ClientSubscriptions.ts:224-277,
// PeerSubscription.ts:42-53); CloseForPeer closes the peer's
// subscriptions (:160-171); Close closes everything (:197-219).
func TestSubscriptionLiveness(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
	closed := make(chan struct{}, 4)
	req := interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}, Closed: func() { closed <- struct{}{} }}
	sub := subscribed(t, s, c, req, 1)
	timeout := 30*time.Second + 2*c.MaximumPeerResponseTime(interaction.SubscriptionProcessingTime, false)
	s.clock.waitTimer(t, timeout)
	s.clock.Advance(timeout - time.Second)
	select {
	case <-sub.Done():
		t.Fatal("closed before its deadline")
	default:
	}
	s.clock.Advance(time.Second)
	select {
	case <-sub.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the subscription did not time out")
	}
	<-closed
	if reg.GetPeer(sub.Peer(), 1) != nil {
		t.Fatal("a timed-out subscription stays registered")
	}

	second := subscribed(t, s, c, req, 2)
	reg.CloseForPeer(second.Peer())
	<-second.Done()
	third := subscribed(t, s, c, req, 3)
	if err := reg.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-third.Done()
	if !reg.IsBlocked() {
		t.Fatal("a closed registry is not blocked")
	}
}

// TestSubscribeReplaces: ReplaceSubscriptions sends keepSubscriptions
// false and closes our earlier subscription with the peer first
// (CI:996-1008, Subscribe.ts:44).
func TestSubscribeReplaces(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
	first := subscribed(t, s, c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}}, 1)
	done := subscribeAsync(c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}, ReplaceSubscriptions: true})
	req := s.next(t)
	sr, err := im.UnmarshalSubscribeRequestTLV(tlv.NewDecoder(req.payload))
	if err != nil || sr.KeepSubscriptions {
		t.Fatalf("SubscribeRequest %+v (%v)", sr, err)
	}
	select {
	case <-first.Done():
	default:
		t.Fatal("the earlier subscription is still open")
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusInvalidAction))
	var se *interaction.StatusError
	if r := await(t, done); !errors.As(r.err, &se) || se.Status != im.StatusInvalidAction {
		t.Fatalf("refused subscribe: %v", r.err)
	}
}

// TestSubscribeRefused: the requests matter.js refuses (CI:981-994,
// :1015-1021, Subscribe.ts:46-60) and a registry-less interaction.
func TestSubscribeRefused(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
	paths := []im.ConcreteAttributePath{onOff}
	for name, req := range map[string]interaction.SubscribeRequest{
		"no paths":            {},
		"ceiling below floor": {Attributes: paths, MinIntervalFloor: interaction.Interval(time.Minute), MaxIntervalCeiling: interaction.Interval(time.Second)},
		"floor out of range":  {Attributes: paths, MinIntervalFloor: interaction.Interval(70000 * time.Second)},
		"negative":            {Attributes: paths, MaxIntervalCeiling: interaction.Interval(-time.Second)},
	} {
		if _, err := c.Subscribe(context.Background(), req); !errors.Is(err, interaction.ErrMalformedRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := interaction.New(s.mgr, s.session, nil).Subscribe(context.Background(), interaction.SubscribeRequest{Attributes: paths}); !errors.Is(err, interaction.ErrNoSubscriptions) {
		t.Errorf("without a registry: %v", err)
	}
	pase := interaction.Bind(interaction.SessionProvider(s.mgr, paseLike(t, s)), interaction.Config{Subscriptions: reg})
	if _, err := pase.Subscribe(context.Background(), interaction.SubscribeRequest{Attributes: paths}); !errors.Is(err, interaction.ErrUncommissioned) {
		t.Errorf("uncommissioned: %v", err)
	}
}
