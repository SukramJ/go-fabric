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
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/im"
)

func TestClientSubscriptionsResetTimer(t *testing.T) {
	t.Parallel()
	t.Run("expires a subscription that times out at the current instant", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
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
	})
}

// TestClientSubscriptionsDelete ports the "delete" group
// (ClientSubscriptionsTest.ts:82-111 at db91886c, #4947): the liveness
// timer stops once the last peer subscription leaves
// (ClientSubscriptions.ts:176-187). matter.js counts the timers named
// "Subscription timeout"; the Go fake clock lists the armed durations, and
// the liveness timer is the one of the full timeout while the clock stands.
func TestClientSubscriptionsDelete(t *testing.T) {
	t.Parallel()
	t.Run("stops the timeout timer when the last subscription leaves", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s := newScripted(t)
			reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
			c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
			timeout := 30*time.Second + 2*c.MaximumPeerResponseTime(interaction.SubscriptionProcessingTime, false)
			first := subscribed(t, s, c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}}, 1)
			second := subscribed(t, s, c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}}, 2)

			first.Close()
			s.clock.waitTimer(t, timeout)

			second.Close()
			synctest.Wait()
			if slices.Contains(s.clock.pending(), timeout) {
				t.Fatalf("the timeout timer is still armed after the last subscription left (pending %v)", s.clock.pending())
			}

			subscribed(t, s, c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}}, 3).Close()
		})
	})
	t.Run("stops the timeout timer when the last subscription leaves as it times out", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s := newScripted(t)
			reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
			c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
			timeout := 30*time.Second + 2*c.MaximumPeerResponseTime(interaction.SubscriptionProcessingTime, false)
			sub := subscribed(t, s, c, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}}, 1)
			s.clock.waitTimer(t, timeout)

			s.clock.Advance(timeout)
			synctest.Wait()
			<-sub.Done()
			if pending := s.clock.pending(); slices.ContainsFunc(pending, func(d time.Duration) bool { return d >= timeout }) {
				t.Fatalf("a timeout timer is still armed (pending %v)", pending)
			}
		})
	})
}

func TestClientSubscriptionsReportStarted(t *testing.T) {
	t.Parallel()
	t.Run("announces the session a report arrived over", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
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
	})
	t.Run("contains an observer that throws", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
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
				Updated:    func(r *interaction.Report) error { updates <- r; return nil },
			}, 5)
			<-updates // the priming report
			s.push(t, 0x7202, im.OpcodeReportData, pushedReport(t, 5, true, false, false, true))
			s.ackPushed(t, expectStatus(t, s, im.StatusSuccess))
			if r := await(t, updates); r.SubscriptionID != 5 {
				t.Fatalf("report %+v", r)
			}
		})
	})
	t.Run("drops observers once closed", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
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
	})
}
