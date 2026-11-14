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
	"github.com/SukramJ/go-fabric/cluster/spec"
	acl "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
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

// openSubscriptions opens n subscriptions req for p and fails the test
// when one is refused.
func openSubscriptions(ctx context.Context, t *testing.T, p *fabrictest.Pair, req interaction.SubscribeRequest, n int) {
	t.Helper()
	for i := range n {
		sub, err := fabrictest.Subscribe(ctx, p, req)
		if err != nil {
			t.Fatalf("subscription %d of %d: %v", i+1, n, err)
		}
		t.Cleanup(sub.Close)
	}
}

// letPass lets d pass on the site's clock the way [fabrictest.Site.Run]
// moves it: to its next deadline each time the site is quiet, until d has
// passed, and then until the site is quiet again. A report that falls due
// on the way is acknowledged before the clock moves on. One
// [fabrictest.Site.Step] over d does not give that: the advance fires the
// report's timer and, in the same call, every retransmission timer the
// report arms behind it ([clocktest.Fake.Advance]), before the peer's
// acknowledgement can arrive, and the device ends the subscription for a
// peer it takes for unreachable.
func letPass(ctx context.Context, t *testing.T, site *fabrictest.Site, d time.Duration) {
	t.Helper()
	until := site.Fake().Now().Add(d)
	passed := func() bool { return !site.Fake().Now().Before(until) }
	if err := site.Eventually(ctx, "the site's clock to pass "+d.String(), passed); err != nil {
		t.Fatal(err)
	}
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
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
	openSubscriptions(ctx, t, p, onOffSubscription(ep), subscription.DefaultMaxSubscriptionsPerFabric)
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
	// A report every five seconds at the latest, so the test's clock has
	// few deadlines to walk ([letPass]).
	const n, maxInterval = 3, 5 * time.Second
	req := onOffSubscription(ep)
	req.MaxIntervalCeiling = interaction.Interval(maxInterval)
	openSubscriptions(ctx, t, p, req, n)
	step := func() {
		t.Helper()
		letPass(ctx, t, site, 2*maxInterval)
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

// TestChunkedACLRewriteKeepsTheFabricsSubscriptions: while an
// administrator's chunked list write of its fabric's ACL is in progress —
// the REPLACE with the empty list in one WriteRequest, the entries
// appended in the next on the same exchange — the fabric's subscriptions
// keep their place through every report due in between, and stay once the
// rewrite has put the entries back (review II, R2-P3-dr-dev-01). The
// subscription's ceiling makes several reports fall due between the
// chunks, well inside the write's idle time (bridge writeTxTTL).
func TestChunkedACLRewriteKeepsTheFabricsSubscriptions(t *testing.T) {
	t.Parallel()
	site, p, pl, ep := pairWithPlug(t, fabrictest.Options{})
	ctx := testContext(t)
	req := onOffSubscription(ep)
	req.MaxIntervalCeiling = interaction.Interval(5 * time.Second)
	sub, err := fabrictest.Subscribe(ctx, p, req)
	if err != nil {
		t.Fatalf("subscription: %v", err)
	}
	t.Cleanup(sub.Close)
	fi := pairFabricIndex(t, p)
	before, err := p.Device.Node().Store().ListACL(ctx, fi)
	if err != nil || len(before) == 0 {
		t.Fatalf("the fabric's ACL before the rewrite: %+v, %v", before, err)
	}

	aclPath := rootAttr(acl.ClusterID, acl.AttrAcl)
	var x *exchange.Exchange
	chunk := func(more bool, w im.AttributeWrite) im.StatusCode {
		t.Helper()
		enc := tlv.NewEncoder()
		im.WriteRequest{MoreChunkedMessages: more, Writes: []im.AttributeWrite{w}}.MarshalTLV(enc,
			func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) { v.Value.(encodeFunc).EncodeTLV(e, tag) })
		payload, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		var resp im.WriteResponse
		err = site.Run(ctx, func(ctx context.Context) error {
			if x == nil {
				var err error
				if x, err = fabrictestseam.Interaction(p).Provider().InitiateExchange(ctx, interaction.ExchangeOptions{}); err != nil {
					return err
				}
			}
			if err := x.Send(ctx, im.OpcodeWriteRequest, payload, exchange.SendOptions{}); err != nil {
				return err
			}
			m, err := x.Next(ctx, exchange.ReceiveOptions{})
			if err != nil {
				return err
			}
			resp, err = im.UnmarshalWriteResponseTLV(tlv.NewDecoder(m.Payload))
			return err
		})
		if err != nil || len(resp.Responses) != 1 {
			t.Fatalf("WriteResponse %+v, %v", resp, err)
		}
		return resp.Responses[0].Status.Status
	}
	t.Cleanup(func() {
		if x != nil {
			x.Close()
		}
	})
	emptyList := encodeFunc(func(enc *tlv.Encoder, tag tlv.Tag) { enc.StartArray(tag); _ = enc.EndContainer() })
	if st := chunk(true, im.AttributeWrite{Path: aclPath, Value: im.AttributeValue{Value: emptyList}}); st != im.StatusSuccess {
		t.Fatalf("the chunk that replaces the ACL with the empty list: %v", st)
	}

	// A change of the subscribed attribute and the keep-alives after it
	// fall due between the chunks.
	if err := p.Device.Act(func(*node.Node) error { pl.srv.Set(true); return nil }); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		letPass(ctx, t, site, 5*time.Second)
		if got := p.Device.Node().Subscriptions().Active(); got != 1 {
			t.Fatalf("between the chunks of the ACL rewrite the device holds %d subscriptions, want 1", got)
		}
	}

	add := aclPath
	add.ListAppend = true
	for i, e := range before {
		entry := encodeFunc(func(enc *tlv.Encoder, tag tlv.Tag) {
			acl.AccessControlEntryStruct{
				Privilege:   acl.AccessControlEntryPrivilegeEnum(e.Privilege),
				AuthMode:    acl.AccessControlEntryAuthModeEnum(e.AuthMode),
				Subjects:    spec.Nullable[[]uint64]{Value: e.Subjects},
				Targets:     spec.Nullable[[]acl.AccessControlTargetStruct]{Null: true},
				FabricIndex: fi,
			}.EncodeTLV(enc, tag)
		})
		if st := chunk(i < len(before)-1, im.AttributeWrite{Path: add, Value: im.AttributeValue{Value: entry}}); st != im.StatusSuccess {
			t.Fatalf("the chunk that appends entry %d of %d: %v", i+1, len(before), st)
		}
	}
	for range 2 {
		letPass(ctx, t, site, 5*time.Second)
	}
	if got := p.Device.Node().Subscriptions().Active(); got != 1 {
		t.Fatalf("after the ACL rewrite the device holds %d subscriptions, want 1", got)
	}
}
