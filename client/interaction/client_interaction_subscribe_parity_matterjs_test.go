// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// The ClientInteraction.close and abandoned-subscribe cases of matter.js's
// ClientInteractionSubscribeTest (packages/protocol/test/action/client/ClientInteractionSubscribeTest.ts
// at db021da0) on the scripted peer: Close (ClientInteraction.ts:264-297),
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
	synctest.Test(t, func(t *testing.T) {
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
	})
}

// TestClientInteractionSubscribeAbandonedReportParity: a priming report
// that arrives after the abort is answered INVALID_SUBSCRIPTION — the
// abandoned establishment awaits it (InteractionMessenger.ts:1116-1129).
func TestClientInteractionSubscribeAbandonedReportParity(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
	})
}

// TestClientInteractionSubscribeClosedWhileProcessingParity: a report
// being processed when the interaction closes is answered
// INVALID_SUBSCRIPTION once, without retransmission, and Close returns
// (:256-292).
func TestClientInteractionSubscribeClosedWhileProcessingParity(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		c := subscribeInteraction(s)
		processing, proceed := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() {
			_, err := c.Subscribe(context.Background(), interaction.SubscribeRequest{
				Attributes: []im.ConcreteAttributePath{onOff},
				Updated: func(*interaction.Report) error {
					close(processing)
					<-proceed
					return nil
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
		synctest.Wait() // Close runs until it waits on the owed report
		if _, err := c.Read(probe, onOff); !errors.Is(err, interaction.ErrClosed) {
			t.Fatalf("read after Close: %v, want ErrClosed", err)
		}
		close(proceed)
		if err := await(t, done); !errors.Is(err, interaction.ErrClosed) {
			t.Fatalf("subscribe: %v, want the close reason", err)
		}
		status := s.next(t)
		if status.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, status.payload) != im.StatusInvalidSubscription {
			t.Fatalf("answer opcode 0x%02X, want StatusResponse INVALID_SUBSCRIPTION", status.proto.Opcode)
		}
		await(t, closed)
		synctest.Wait() // every goroutine of the bubble has settled: anything owed is sent
		select {
		case b := <-s.conn.ch:
			t.Fatalf("a retransmission after Close (%d bytes)", len(b))
		default:
		}
	})
}

// closesOnUnacknowledgedFinalStatus subscribes with req on the scripted
// peer and reports whether an unacknowledged final report status ends the
// subscription — closesOnUnacknowledgedFinalStatus of
// ClientInteractionSubscribeTest.ts:199-218 at db91886c.
func closesOnUnacknowledgedFinalStatus(t *testing.T, req interaction.SubscribeRequest) bool {
	t.Helper()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
	sub := subscribed(t, s, c, req, 1)
	interaction.ReportInterrupted(sub, "final-unacknowledged")
	select {
	case <-sub.Done():
		return true
	default:
		sub.Close()
		return false
	}
}

// onOffCluster is ON_OFF of ClientInteractionSubscribeTest.ts:219: endpoint
// 1, cluster OnOff.
var onOffCluster = im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0006, HasEndpoint: true, HasCluster: true}

// TestClientInteractionSubscribeCompactPrimingParity ports the "compact
// priming" group (ClientInteractionSubscribeTest.ts:387-422 at b25ed960,
// #4943): the estimate primesCompactly (ClientInteraction.ts:82-99) makes
// of the subscribe request, seen through whether an unacknowledged final
// report status closes the subscription.
func TestClientInteractionSubscribeCompactPrimingParity(t *testing.T) {
	t.Parallel()
	onOffFilter := im.DataVersionFilter{Endpoint: 1, Cluster: 0x0006, DataVersion: 1}
	cases := []struct {
		name string
		req  interaction.SubscribeRequest
		want bool
	}{
		{
			"counts a subscription of single attributes as compact",
			interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}},
			true,
		},
		{
			"counts a subscription of all attributes of a cluster as not compact",
			interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOffCluster}},
			false,
		},
		{
			"counts a wildcard subscription without DataVersionFilters as not compact",
			interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{{}}},
			false,
		},
		{
			"counts a wildcard subscription whose DataVersionFilters all fit as compact",
			interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{{}}, DataVersionFilters: []im.DataVersionFilter{onOffFilter}},
			true,
		},
		{
			"counts a wildcard subscription whose DataVersionFilters do not all fit as not compact",
			interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{{}}, DataVersionFilters: manyFilters(200)},
			false,
		},
		{
			"counts a subscription of events without an event filter as not compact",
			interaction.SubscribeRequest{Events: []im.ConcreteEventPath{{}}},
			false,
		},
		{
			"counts a subscription of events with an event filter as compact",
			interaction.SubscribeRequest{Events: []im.ConcreteEventPath{{}}, EventFilters: []im.EventMinimumNumber{{EventMin: 5}}},
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := closesOnUnacknowledgedFinalStatus(t, tc.req); got != tc.want {
				t.Fatalf("closed %v, want %v", got, tc.want)
			}
		})
	}
}

// manyFilters is n DataVersionFilters of OnOff on endpoints 0…n-1 at the
// largest data version (ClientInteractionSubscribeTest.ts:408-411).
func manyFilters(n int) []im.DataVersionFilter {
	filters := make([]im.DataVersionFilter, n)
	for i := range filters {
		filters[i] = im.DataVersionFilter{Endpoint: uint16(i), Cluster: 0x0006, DataVersion: 1<<32 - 1} //nolint:gosec // n stays below 2^16
	}
	return filters
}
