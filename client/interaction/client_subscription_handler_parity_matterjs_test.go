// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js
// packages/protocol/test/action/client/subscription/ClientSubscriptionHandlerTest.ts
// at db91886c (#4943, #5046): a subscription closed when the peer never
// acknowledged our status to its report or to a report chunk, or never
// sent the next chunk after acknowledging our status to one.
//
// matter.js drives the handler with a fake exchange whose StatusResponse
// send rejects with PeerAckMissingError; here the peer of the scripted rig
// pushes a keepalive and never acknowledges our StatusResponse, so the
// exchange's MRP gives up as it does on the wire. compactPriming is the
// estimate of the subscribe request (ClientInteraction.ts:88-99 at
// b25ed960): a subscription to the concrete OnOff attribute primes
// compactly, a wildcard one without DataVersionFilters does not.

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
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

// handlerLog is a slog destination the chunk-missing cases read back.
type handlerLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *handlerLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *handlerLog) has(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Contains(b.buf.String(), key)
}

// chunkRig subscribes, without compact priming, on a handler rig whose
// registry logs to log, has the peer push the first chunk of a report with
// more to follow, and acknowledges our Success status to it: the next chunk
// is what the peer owes.
func chunkRig(t *testing.T, log *handlerLog, xid uint16) (*scripted, *interaction.Subscriptions, *interaction.Subscription, time.Duration, msg) {
	t.Helper()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{
		Clock:  s.clock,
		Logger: slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	reg.Register(s.mgr)
	t.Cleanup(func() { s.mgr.SetProtocolHandler(im.InteractionModelProtocolID, nil) })
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
	sub := subscribed(t, s, c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{{}}}, 5)
	s.push(t, xid, im.OpcodeReportData, pushedReport(t, 5, true, true, false, true))
	status := expectStatus(t, s, im.StatusSuccess)
	s.ackPushed(t, status)
	return s, reg, sub, 30*time.Second + 2*c.MaximumPeerResponseTime(interaction.SubscriptionProcessingTime, false), status
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
	// ClientSubscriptionHandlerTest.ts:167-175 at db91886c: the wait for the
	// next chunk ends without one, the report is interrupted chunk-missing,
	// which closes the subscription whatever its priming, and the report's
	// error is swallowed.
	t.Run("closes a subscription whose peer acknowledged our status to a chunk but sent no next chunk", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			var log handlerLog
			s, _, sub, timeout, _ := chunkRig(t, &log, 0x7311)
			unanswered(t, s, timeout)
			if !isClosed(sub) {
				t.Fatal("the subscription stayed open")
			}
			if log.has("report_failed") {
				t.Fatal("the missing chunk surfaced as a failed report")
			}
		})
	})
	// :177-184: the wait for the next chunk fails for another reason — here
	// the peer answers with a StatusResponse Failure in place of the chunk —
	// so no interruption is recorded, the subscription is kept and the error
	// surfaces.
	t.Run("keeps a subscription when waiting for the next chunk fails for another reason", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			var log handlerLog
			s, _, sub, _, status := chunkRig(t, &log, 0x7312)
			s.continueOn(t, status, im.OpcodeStatusResponse, statusResponse(t, im.StatusFailure))
			synctest.Wait()
			if isClosed(sub) {
				t.Fatal("the subscription closed")
			}
			if !log.has("report_failed") {
				t.Fatal("the failure did not surface")
			}
			sub.Close()
		})
	})
	// :186-197: the next chunk goes missing while the registry shuts down —
	// the subscription is kept.
	t.Run("keeps a subscription whose next chunk goes missing during shutdown", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			var log handlerLog
			s, reg, sub, timeout, _ := chunkRig(t, &log, 0x7313)
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
