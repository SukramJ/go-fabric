// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
	"github.com/SukramJ/go-fabric/node"
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
// itself (PeerExchangeProvider.ts:73-81, Peer.ts:418-463), as matter.js
// retries no failed interaction (ClientInteraction.ts:1247-1264,
// InteractionMessenger.ts:1096-1100). A sustained subscription comes back
// on a new session by itself (ClientInteraction.ts:1155-1159,
// PeerAddressMonitor.ts:255-261). Owner decision 49.
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
	t.Run("subscription", func(t *testing.T) {
		t.Parallel()
		site, p, pl, ep := pairWithPlug(t, fabrictest.Options{})
		ctx := testContext(t)
		pr := fabrictestseam.Peer(p)
		old := pr.NewestSession()
		reports := make(chan *interaction.Report, 64)
		sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOffPath(ep)}, MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Minute),
			Updated: func(r *interaction.Report) error { reports <- r; return nil },
			Sustain: true,
		})
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		defer sub.Close()
		restartSilently(ctx, t, site, p)
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
		if !old.IsPeerLost() {
			t.Fatal("the session the device lost was not dropped")
		}
		if s := pr.NewestSession(); s == nil || s == old {
			t.Fatalf("the subscription reported on %v, want a new session", s)
		}
	})
}
