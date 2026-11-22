// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/client/peer"
	"thingfabric.dev/go-fabric/cluster/onoff"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/fabrictestseam"
	"thingfabric.dev/go-fabric/node"
)

// restartSilently stops the pair's device with every datagram it sends
// meanwhile lost — no CloseSession reaches the controller, as when the
// device loses power — and starts it again on its store and address.
func restartSilently(ctx context.Context, t *testing.T, site *fabrictest.Site, p *fabrictest.Pair) {
	t.Helper()
	dev := p.Device.Addr().String()
	site.Net().Filter(func(d fabrictest.Datagram) bool { return d.From.String() != dev })
	if err := p.Device.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	site.Net().Filter(nil)
	if err := p.Device.Start(ctx); err != nil {
		t.Fatal(err)
	}
}

// sendGiveUpBudget is the longest a reliable send of an interaction on s
// can wait before it fails: MaxTransmissions transmissions at the peer's
// slower interval with the maximum jitter, then the final wait for the
// peer's answer (client/exchange/exchange.go retransmit, budgetSpentLocked
// and backoffForLocked), padded with the larger of the controller's own
// network margin (fabrictest/controller.go build: the fast profile,
// LocalFixedMrpBackoff unset) and the peer's IM margin
// (client/interaction/provider.go InitiateExchange), with the default
// expected processing time a read sends with (interaction/read.go,
// exchange.go expectedProcessingTimeLocked).
func sendGiveUpBudget(t *testing.T, pr *peer.Peer, s *exchange.Session) time.Duration {
	t.Helper()
	own, err := pr.Networks().Get(peer.NetworkFast)
	if err != nil {
		t.Fatal(err)
	}
	margin, _ := exchange.MarginFor(&pr.Networks().ForPeer(pr).Margins, im.InteractionModelProtocolID)
	pad := exchange.Pad{Additional: max(own.AdditionalMrpDelay, margin)}
	iv := exchange.IntervalsOf(s.Parameters())
	var budget time.Duration
	for i := range exchange.MaxTransmissions {
		budget += max(exchange.RetransmissionIntervalPadded(i, iv, true, 1, pad), exchange.RetransmissionIntervalPadded(i, iv, false, 1, pad))
	}
	local := exchange.IntervalsOf(exchange.DefaultLocalParameters())
	return budget + exchange.MaxPeerResponseTimePadded(nil, false, local, exchange.DefaultExpectedProcessingTime, pad)
}

// TestControllerRecoversFromASilentRestart: a device that restarts without
// a CloseSession leaves the controller holding a session the device no
// longer knows. The first interaction on it fails as unresponsive within
// the MRP budget of the session's parameters and drops the session (every
// session of the peer created before the exchange — matter.js
// MessageExchange.ts:643-650 → ExchangeManager.ts:621-644 →
// SessionManager.ts:727-746); the next one establishes CASE again by
// itself (PeerExchangeProvider.ts:73-85, Peer.ts:418-463), as matter.js
// retries no failed interaction (ClientInteraction.ts:1247-1264,
// InteractionMessenger.ts:1096-1100). A sustained subscription comes back
// on a new session by itself (ClientInteraction.ts:1155-1159,
// PeerAddressMonitor.ts:258-260). Owner decision 49.
func TestControllerRecoversFromASilentRestart(t *testing.T) {
	t.Parallel()
	t.Run("read", func(t *testing.T) {
		t.Parallel()
		site, p, _, ep := pairWithPlug(t, fabrictest.Options{})
		ctx := testContext(t)
		pr := fabrictestseam.Peer(p)
		old := pr.NewestSession()
		if old == nil {
			t.Fatal("no session after commissioning")
		}
		budget := sendGiveUpBudget(t, pr, old)
		restartSilently(ctx, t, site, p)
		start := site.Fake().Now()
		_, err := fabrictest.Read(ctx, p, onOffPath(ep))
		noticed := site.Fake().Now().Sub(start)
		var unresponsive *exchange.PeerUnresponsiveError
		if !errors.As(err, &unresponsive) {
			t.Fatalf("the read on the session the device lost: %v, want a PeerUnresponsiveError", err)
		}
		if noticed > budget {
			t.Fatalf("the read failed after %v, past the MRP budget %v of the session's parameters", noticed, budget)
		}
		// Lost, so no exchange takes it; it closes once the failed
		// exchange has (NodeSession.ts deferredClose, exchange/session.go
		// handlePeerLoss).
		if !old.IsPeerLost() {
			t.Fatal("the session the device lost was not dropped by the failed read")
		}
		t.Logf("noticed after %v, MRP budget %v", noticed, budget)
		if _, err := fabrictest.Read(ctx, p, onOffPath(ep)); err != nil {
			t.Fatalf("the next read: %v, want it answered on a new session", err)
		}
		if s := pr.NewestSession(); s == nil || s == old || s.IsInbound() {
			t.Fatalf("the next read ran on %v, want a new CASE session of the controller's", s)
		}
	})
	// The subscription's two recovery paths, each ordered against the
	// device's persistence of the subscription, which it records after
	// the SubscribeResponse (bridge/subscription_persistence.go): without
	// that order the case took one path or the other by timing
	// (R2-P6-test-3101, F-db021da0-58, N5b-guard-1).
	t.Run("subscription", func(t *testing.T) {
		t.Parallel()
		// Nothing persisted at the restart: the device has no former
		// subscription to re-establish, the controller notices the loss
		// itself, drops the old session and subscribes again on a new
		// session it opens.
		t.Run("controller recovers", func(t *testing.T) {
			t.Parallel()
			sp := subscribedPair(t)
			site, p, pl, ep, old, reports := sp.site, sp.p, sp.pl, sp.ep, sp.old, sp.reports
			ctx := testContext(t)
			waitPersistedSubscription(ctx, t, site, p)
			if err := p.Device.Act(func(n *node.Node) error {
				return n.Store().ClearServerSubscriptions(ctx)
			}); err != nil {
				t.Fatal(err)
			}
			restartSilently(ctx, t, site, p)
			waitOnReported(ctx, t, site, p, pl, ep, reports)
			if !old.IsPeerLost() {
				t.Fatal("the session the device lost was not dropped")
			}
			pr := fabrictestseam.Peer(p)
			s := pr.NewestSession()
			if s == nil || s == old {
				t.Fatalf("the subscription reported on %v, want a new session", s)
			}
			if !fabrictestseam.Peers(p.Controller).Initiated(s) {
				t.Fatal("the new session is not the controller's: the device re-established a subscription it did not persist")
			}
		})
		// The subscription persisted before the restart: the restarted
		// device re-establishes it (node.go ReestablishFormerSubscriptions)
		// on a session the device opens, and the report arrives there. The
		// session from before the restart stays: a session the peer
		// establishes does not end the controller's older ones
		// (R2-P6-par-3102, owner decision 239).
		t.Run("device resumes", func(t *testing.T) {
			t.Parallel()
			sp := subscribedPair(t)
			site, p, pl, ep, old, reports := sp.site, sp.p, sp.pl, sp.ep, sp.old, sp.reports
			ctx := testContext(t)
			waitPersistedSubscription(ctx, t, site, p)
			restartSilently(ctx, t, site, p)
			waitOnReported(ctx, t, site, p, pl, ep, reports)
			pr := fabrictestseam.Peer(p)
			s := pr.NewestSession()
			if s == nil || s == old {
				t.Fatalf("the subscription reported on %v, want a new session", s)
			}
			if fabrictestseam.Peers(p.Controller).Initiated(s) {
				t.Fatal("the new session is the controller's, want the one the restarted device opened to re-establish its subscription")
			}
			if old.IsPeerLost() || old.IsClosed() {
				t.Fatalf("the session from before the restart: lost %v, closed %v; want it kept", old.IsPeerLost(), old.IsClosed())
			}
		})
	})
}

// subscribed is a commissioned pair whose controller holds a sustained
// subscription to the plug's OnOff, with the session it was made on and
// the channel its reports arrive on.
type subscribed struct {
	site    *fabrictest.Site
	p       *fabrictest.Pair
	pl      *plug
	ep      uint16
	old     *exchange.Session
	reports chan *interaction.Report
}

func subscribedPair(t *testing.T) subscribed {
	t.Helper()
	site, p, pl, ep := pairWithPlug(t, fabrictest.Options{})
	ctx := testContext(t)
	old := fabrictestseam.Peer(p).NewestSession()
	if old == nil {
		t.Fatal("no session after commissioning")
	}
	reports := make(chan *interaction.Report, 64)
	sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{onOffPath(ep)}, MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Minute),
		Updated: func(r *interaction.Report) error { reports <- r; return nil },
		Sustain: true,
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(sub.Close)
	return subscribed{site: site, p: p, pl: pl, ep: ep, old: old, reports: reports}
}

// waitPersistedSubscription waits until the device has stored the
// subscription for re-establishment.
func waitPersistedSubscription(ctx context.Context, t *testing.T, site *fabrictest.Site, p *fabrictest.Pair) {
	t.Helper()
	if err := site.Eventually(ctx, "the device to persist the subscription", func() bool {
		persisted := false
		_ = p.Device.Act(func(n *node.Node) error {
			rows, err := n.Store().LoadServerSubscriptions(ctx)
			persisted = err == nil && len(rows) > 0
			return nil
		})
		return persisted
	}); err != nil {
		t.Fatal(err)
	}
}

// waitOnReported sets the plug on and waits until a report carried it.
func waitOnReported(ctx context.Context, t *testing.T, site *fabrictest.Site, p *fabrictest.Pair, pl *plug, ep uint16, reports chan *interaction.Report) {
	t.Helper()
	if err := p.Device.Act(func(*node.Node) error { pl.srv.Set(true); return nil }); err != nil {
		t.Fatal(err)
	}
	var on bool
	if err := site.Eventually(ctx, "OnOff true reported after the restart", func() bool {
		for {
			select {
			case r := <-reports:
				v, ok := r.Value(ep, onoff.ClusterID, onoff.AttrOnOff)
				on = on || (ok && v.El.Bool)
			default:
				return on
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
}
