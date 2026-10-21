// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/protocol/test/action/client/subscription/ClientSubscriptionsTest.ts at
// 862ad75f: the controller's subscription liveness timer and the
// report-started observer.
//
// matter.js drives ClientSubscriptions with a fake PeerSubscription and
// emits reportStarted by hand; the Go registry has one ReportStarted
// callback that the inbound report handler calls, so these cases run a
// real subscription on the scripted rig and push its reports.

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
)

func TestClientSubscriptionsResetTimer(t *testing.T) {
	t.Parallel()
	t.Run("expires a subscription that times out at the current instant", func(t *testing.T) {
		t.Parallel()
		// The Go subscription's timeout is derived from the granted max
		// interval (PeerSubscription.ts:38-40), not set directly: the
		// timer is armed for exactly that timeout (timeoutAt = now +
		// timeout, ClientSubscriptionsTest.ts:51) and expires at the
		// instant it is reached (:53-55).
		s := newScripted(t)
		reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
		timedOut := make(chan struct{}, 2)
		sub := subscribed(t, s, c, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOff},
			Closed:     func() { timedOut <- struct{}{} },
		}, 1)
		timeout := 30*time.Second + 2*c.MaximumPeerResponseTime(interaction.SubscriptionProcessingTime, false)
		s.clock.waitTimer(t, timeout)
		s.clock.Advance(timeout)
		select {
		case <-sub.Done():
		default:
			t.Fatal("the subscription did not expire at its deadline")
		}
		if len(timedOut) != 1 {
			t.Fatalf("timed out %d times, want 1", len(timedOut))
		}
	})
}

func TestClientSubscriptionsReportStarted(t *testing.T) {
	t.Parallel()
	t.Run("announces the session a report arrived over", func(t *testing.T) {
		t.Parallel()
		s, _, c, started := handlerRig(t)
		subscribed(t, s, c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}}, 5)
		s.push(t, 0x7201, im.OpcodeReportData, pushedReport(t, 5, true, false, false, true))
		s.ackPushed(t, expectStatus(t, s, im.StatusSuccess))
		if got := await(t, started); got != s.session {
			t.Fatal("announced another session")
		}
		if len(started) != 0 {
			t.Fatalf("announced %d more times", len(started))
		}
	})
	t.Run("contains an observer that throws", func(t *testing.T) {
		t.Parallel()
		// Go has one callback, not a list of observers: a panic in it must
		// not abort the report being read (ClientSubscriptionsTest.ts:131-133).
		s := newScripted(t)
		reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock, ReportStarted: func(*exchange.Session) {
			panic("observer is broken")
		}})
		reg.Register(s.mgr)
		t.Cleanup(func() { s.mgr.SetProtocolHandler(im.InteractionModelProtocolID, nil) })
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
		updates := make(chan *interaction.Report, 4)
		subscribed(t, s, c, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOff},
			Updated:    func(r *interaction.Report) { updates <- r },
		}, 5)
		<-updates // the priming report
		s.push(t, 0x7202, im.OpcodeReportData, pushedReport(t, 5, true, false, false, true))
		s.ackPushed(t, expectStatus(t, s, im.StatusSuccess))
		if r := await(t, updates); r.SubscriptionID != 5 {
			t.Fatalf("report %+v", r)
		}
	})
	t.Run("drops observers once closed", func(t *testing.T) {
		t.Parallel()
		s, reg, c, started := handlerRig(t)
		subscribed(t, s, c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}}, 5)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := reg.Close(ctx); err != nil {
			t.Fatal(err)
		}
		s.push(t, 0x7203, im.OpcodeReportData, pushedReport(t, 5, true, false, false, true))
		s.ackPushed(t, expectStatus(t, s, im.StatusInvalidSubscription))
		if len(started) != 0 {
			t.Fatalf("announced %d times after close", len(started))
		}
	})
}
