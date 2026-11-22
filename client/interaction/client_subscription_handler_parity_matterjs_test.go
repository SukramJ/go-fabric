// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js
// packages/protocol/test/action/client/subscription/ClientSubscriptionHandlerTest.ts
// at b25ed960 (#4943): a subscription closed when the peer never
// acknowledged our status to its report or to a report chunk.
//
// matter.js drives the handler with a fake exchange whose StatusResponse
// send rejects with PeerAckMissingError; here the peer of the scripted rig
// pushes a keepalive and never acknowledges our StatusResponse, so the
// exchange's MRP gives up as it does on the wire. compactPriming is the
// estimate of the subscribe request (ClientInteraction.ts:88-99 at
// b25ed960): a subscription to the concrete OnOff attribute primes
// compactly, a wildcard one without DataVersionFilters does not.

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/im"
)

// keepaliveRig subscribes on the handler rig — to OnOff when compact, to
// every attribute otherwise — and returns the rig, its registry, the
// subscription and its liveness timeout.
func keepaliveRig(t *testing.T, compact bool) (*scripted, *interaction.Subscriptions, *interaction.Subscription, time.Duration) {
	t.Helper()
	s, reg, c, _ := handlerRig(t)
	paths := []im.ConcreteAttributePath{{}}
	if compact {
		paths = []im.ConcreteAttributePath{onOff}
	}
	sub := subscribed(t, s, c, interaction.SubscribeRequest{Attributes: paths}, 5)
	return s, reg, sub, 30*time.Second + 2*c.MaximumPeerResponseTime(interaction.SubscriptionProcessingTime, false)
}

// unanswered lets the scripted peer stay silent until the exchange's MRP
// gives up on the message we sent and the handler is done: it advances the
// fake clock timer by timer, dropping our retransmissions, until the
// handler re-armed the liveness timer for its full timeout at its end, or
// no timer is left (the last subscription closed, or the registry is
// blocked). It must run inside synctest.Test.
func unanswered(t *testing.T, s *scripted, timeout time.Duration) {
	t.Helper()
	advanced := false
	for range 200 {
		synctest.Wait()
		for len(s.conn.ch) > 0 {
			<-s.conn.ch
		}
		pending := s.clock.pending()
		if len(pending) == 0 || slices.Min(pending) == timeout || (advanced && slices.Contains(pending, timeout)) {
			return
		}
		s.clock.Advance(slices.Min(pending))
		advanced = true
	}
	t.Fatalf("timers still armed: %v", s.clock.pending())
}

// isClosed reports whether sub closed, once the bubble settled.
func isClosed(sub *interaction.Subscription) bool {
	synctest.Wait()
	select {
	case <-sub.Done():
		return true
	default:
		return false
	}
}

func TestClientSubscriptionHandlerParity(t *testing.T) {
	t.Parallel()
	t.Run("closes a subscription whose report status the peer never acknowledged", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, _, sub, timeout := keepaliveRig(t, true)
			s.push(t, 0x7301, im.OpcodeReportData, pushedReport(t, 5, false, false, false, false))
			expectStatus(t, s, im.StatusSuccess)
			unanswered(t, s, timeout)
			if !isClosed(sub) {
				t.Fatal("the subscription stayed open")
			}
		})
	})
	t.Run("keeps a subscription without compact priming whose final report status the peer never acknowledged", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, _, sub, timeout := keepaliveRig(t, false)
			s.push(t, 0x7302, im.OpcodeReportData, pushedReport(t, 5, false, false, false, false))
			expectStatus(t, s, im.StatusSuccess)
			unanswered(t, s, timeout)
			if isClosed(sub) {
				t.Fatal("the subscription closed")
			}
			sub.Close()
		})
	})
	t.Run("closes a subscription whose status to a report chunk the peer never acknowledged", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, _, sub, timeout := keepaliveRig(t, false)
			s.push(t, 0x7303, im.OpcodeReportData, pushedReport(t, 5, false, true, false, false))
			expectStatus(t, s, im.StatusSuccess)
			unanswered(t, s, timeout)
			if !isClosed(sub) {
				t.Fatal("the subscription stayed open")
			}
		})
	})
	t.Run("keeps a subscription whose report status the peer acknowledged", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, _, sub, timeout := keepaliveRig(t, true)
			s.push(t, 0x7304, im.OpcodeReportData, pushedReport(t, 5, false, false, false, false))
			s.ackPushed(t, expectStatus(t, s, im.StatusSuccess))
			s.clock.waitTimer(t, timeout)
			if isClosed(sub) {
				t.Fatal("the subscription closed")
			}
			sub.Close()
		})
	})
	t.Run("keeps a subscription when the report status fails for another reason", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// matter.js rejects the send with a PeerUnresponsiveError that is
			// no PeerAckMissingError; here the socket fails the send.
			s, _, sub, _ := keepaliveRig(t, true)
			s.conn.mu.Lock()
			s.conn.fail = true
			s.conn.mu.Unlock()
			s.push(t, 0x7305, im.OpcodeReportData, pushedReport(t, 5, false, false, false, false))
			if isClosed(sub) {
				t.Fatal("the subscription closed")
			}
			sub.Close()
		})
	})
	t.Run("keeps a subscription whose report status goes unacknowledged during shutdown", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, reg, sub, timeout := keepaliveRig(t, true)
			s.push(t, 0x7306, im.OpcodeReportData, pushedReport(t, 5, false, false, false, false))
			expectStatus(t, s, im.StatusSuccess)
			blocked := make(chan error, 1)
			go func() { blocked <- reg.BlockNewActivity(context.Background()) }()
			unanswered(t, s, timeout)
			if err := await(t, blocked); err != nil {
				t.Fatal(err)
			}
			if isClosed(sub) {
				t.Fatal("the subscription closed")
			}
			sub.Close()
		})
	})
}
