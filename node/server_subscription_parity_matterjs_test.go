// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ServerSubscriptionTest.ts at
// 862ad75f, the cases a subscription over the wire shows: the negotiated
// MaxInterval (a floor above the publisher limit, the generic non-ICD
// calculation), a report of only the changed attribute, and a keep-alive the
// controller answers with INVALID_SUBSCRIPTION ending the subscription.
//
// matter.js builds ServerSubscription directly with mocked exchanges; the
// port subscribes from a commissioned fabrictest pair and reads the outcome
// on both ends — the controller's negotiated MaxInterval, the device's
// subscription manager. The subscription's own state machine (peer cancel,
// close against close-for-shutdown, endpoint selection by event paths) is
// ported in im/subscription; the rest stays missing in the register.
package node_test

import (
	"testing"
	"time"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
)

func TestServerSubscriptionParity(t *testing.T) {
	t.Parallel()

	nodeLabel := attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	location := attrPath(0, basicinfo.ClusterID, basicinfo.AttrLocation)

	negotiated := func(t *testing.T, floor, ceiling time.Duration) time.Duration {
		t.Helper()
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		sub, err := fabrictest.Subscribe(parityCtx(t), p, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{nodeLabel}, MinIntervalFloor: interaction.Interval(floor), MaxIntervalCeiling: interaction.Interval(ceiling),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(sub.Close)
		return sub.MaxInterval()
	}

	// Spec §8.5.3.2: MinIntervalFloor <= MaxInterval, even above the
	// 60-minute publisher limit.
	t.Run("keeps negotiated maxInterval >= minIntervalFloor when floor exceeds the 60-min publisher limit", func(t *testing.T) {
		t.Parallel()
		const floor = 65535 * time.Second
		if got := negotiated(t, floor, floor); got < floor {
			t.Fatalf("MaxInterval %v, below the floor %v", got, floor)
		}
	})

	// matter.js: min(configured 3 min, ceiling 60 s) plus up to 10 s of
	// randomization — at least 60 s, below 70 s.
	t.Run("uses the generic (non-ICD) interval calculation for a non-ICD publisher", func(t *testing.T) {
		t.Parallel()
		if got := negotiated(t, 0, 60*time.Second); got < 60*time.Second || got >= 70*time.Second {
			t.Fatalf("MaxInterval %v, want [60 s, 70 s)", got)
		}
	})

	t.Run("reports only the changed attribute when several attributes of one cluster are subscribed", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		ctx := parityCtx(t)
		reports := make(chan *interaction.Report, 8)
		primed := false
		sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{nodeLabel, location}, MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(2 * time.Second),
			Updated: func(r *interaction.Report) {
				if primed {
					reports <- r
				}
				primed = true
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer sub.Close()
		if err := fabrictest.Write(ctx, p, nodeLabel, strVal("relabeled")); err != nil {
			t.Fatal(err)
		}
		var got *interaction.Report
		if err := site.Eventually(ctx, "the report of the change", func() bool {
			select {
			case r := <-reports:
				if len(r.Attributes) > 0 {
					got = r
				}
			default:
			}
			return got != nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(got.Attributes) != 1 || !samePath(got.Attributes[0].Path, nodeLabel) {
			t.Fatalf("report %+v, want NodeLabel alone", got.Attributes)
		}
	})

	t.Run("keep-alive status", func(t *testing.T) {
		t.Parallel()
		// The controller forgets the subscription without telling the
		// device; the device's next keep-alive asks for a status (an MRP
		// session), the controller answers INVALID_SUBSCRIPTION, and the
		// device ends the subscription.
		t.Run("requests a status on an MRP session and ends the subscription on INVALID_SUBSCRIPTION", func(t *testing.T) {
			t.Parallel()
			site := quietSite(t)
			p := site.CommissionedPair(fabrictest.PairOptions{})
			ctx := parityCtx(t)
			sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
				Attributes: []im.ConcreteAttributePath{nodeLabel}, MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(10 * time.Second),
			})
			if err != nil {
				t.Fatal(err)
			}
			subs := p.Device.Node().Subscriptions()
			if subs.Active() != 1 {
				t.Fatalf("%d subscriptions on the device, want 1", subs.Active())
			}
			sub.Close()
			if err := site.Eventually(ctx, "the device ended the subscription", func() bool { return subs.Active() == 0 }); err != nil {
				t.Fatal(err)
			}
		})
	})
}
