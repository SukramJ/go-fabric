// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	acl "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/store"
)

// onOffSubscription is a subscription to the plug's OnOff attribute with a
// one-minute ceiling.
func onOffSubscription(ep uint16) interaction.SubscribeRequest {
	return interaction.SubscribeRequest{
		Attributes:       []im.ConcreteAttributePath{onOffPath(ep)},
		MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Minute),
		Updated: func(*interaction.Report) error { return nil },
	}
}

// openSubscriptions opens n subscriptions for p and fails the test when
// one is refused.
func openSubscriptions(ctx context.Context, t *testing.T, p *fabrictest.Pair, ep uint16, n int) {
	t.Helper()
	for i := range n {
		sub, err := fabrictest.Subscribe(ctx, p, onOffSubscription(ep))
		if err != nil {
			t.Fatalf("subscription %d of %d: %v", i+1, n, err)
		}
		t.Cleanup(sub.Close)
	}
}

// TestFabricAtItsSubscriptionQuotaMakesRoom: a SubscribeRequest of a
// subject that may read what it asks for is admitted when its fabric holds
// its quota of subscriptions already — the fabric's oldest subscription
// ends instead, connectedhomeip's rule
// (src/app/InteractionModelEngine.cpp:1341-1350 at 6170af84) — so a subject
// that took the fabric's every place does not keep the fabric's other
// controllers from subscribing (review II, R2-P3-admin-03, owner decision
// 53). The site builds one controller per fabric, so the fabric's
// administrator is the same node granted Administer after it took every
// place as a View subject.
func TestFabricAtItsSubscriptionQuotaMakesRoom(t *testing.T) {
	t.Parallel()
	_, p, _, ep := pairWithPlug(t, fabrictest.Options{})
	ctx := testContext(t)
	if err := p.Grant(ctx, store.PrivilegeView); err != nil {
		t.Fatal(err)
	}
	openSubscriptions(ctx, t, p, ep, subscription.DefaultMaxSubscriptionsPerFabric)
	if got := p.Device.Node().Subscriptions().Active(); got != subscription.DefaultMaxSubscriptionsPerFabric {
		t.Fatalf("the device holds %d subscriptions, want the fabric's quota %d", got, subscription.DefaultMaxSubscriptionsPerFabric)
	}
	// A request for a path the subject may not read takes nobody's place
	// and is answered as it is with room: connectedhomeip checks the
	// access before it makes room (InteractionModelEngine.cpp:861-875).
	denied := onOffSubscription(ep)
	denied.Attributes = []im.ConcreteAttributePath{rootAttr(acl.ClusterID, acl.AttrAcl)}
	var se *interaction.StatusError
	if _, err := fabrictest.Subscribe(ctx, p, denied); !errors.As(err, &se) || se.Status != im.StatusInvalidAction {
		t.Fatalf("the View subject's subscription to the ACL at the quota: %v, want INVALID_ACTION", err)
	}
	if got := p.Device.Node().Subscriptions().Active(); got != subscription.DefaultMaxSubscriptionsPerFabric {
		t.Fatalf("after a refused request the device holds %d subscriptions, want %d", got, subscription.DefaultMaxSubscriptionsPerFabric)
	}

	if err := p.Grant(ctx, store.PrivilegeAdminister); err != nil {
		t.Fatal(err)
	}
	sub, err := fabrictest.Subscribe(ctx, p, onOffSubscription(ep))
	if err != nil {
		t.Fatalf("the fabric's SubscribeRequest at its quota: %v, want SUCCESS", err)
	}
	t.Cleanup(sub.Close)
	if got := p.Device.Node().Subscriptions().Active(); got != subscription.DefaultMaxSubscriptionsPerFabric {
		t.Fatalf("after the fabric's next subscription the device holds %d, want %d (the oldest ended)", got, subscription.DefaultMaxSubscriptionsPerFabric)
	}
}

// TestSubscriptionEndsWhenItsSubjectReadsNothing: a subscription whose
// subject has lost every path it subscribed to — its ACL entry is gone —
// ends at its next report, and gives its place in the fabric's quota back;
// while the subject may read the path, the reports keep it alive (review
// II, R2-P3-admin-03, owner decision 53).
func TestSubscriptionEndsWhenItsSubjectReadsNothing(t *testing.T) {
	t.Parallel()
	site, p, _, ep := pairWithPlug(t, fabrictest.Options{})
	ctx := testContext(t)
	if err := p.Grant(ctx, store.PrivilegeView); err != nil {
		t.Fatal(err)
	}
	const n = 3
	openSubscriptions(ctx, t, p, ep, n)
	step := func() {
		t.Helper()
		for range 2 {
			if err := site.Step(ctx, time.Minute); err != nil {
				t.Fatalf("step: %v", err)
			}
		}
	}
	step() // the keep-alives of a subject that may read its path
	if got := p.Device.Node().Subscriptions().Active(); got != n {
		t.Fatalf("after the keep-alives the device holds %d subscriptions, want %d", got, n)
	}

	fi := pairFabricIndex(t, p)
	if err := p.Device.Node().Store().ReplaceACL(ctx, fi, []store.ACLEntry{{
		FabricIndex: fi, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{0xA0AD},
	}}); err != nil {
		t.Fatal(err)
	}
	step()
	if got := p.Device.Node().Subscriptions().Active(); got != 0 {
		t.Fatalf("after the subject lost its ACL entry and its subscriptions' reports were due, the device holds %d subscriptions, want 0", got)
	}
}
