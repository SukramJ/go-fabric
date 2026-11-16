// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// TestReestablishedRecordAnswersForThisNodeOnly: a record written before
// the node-clearing of a subscription's paths (R2-P2-alloc-07,
// R2-P4-imread-07) restores as a fresh subscription would be admitted —
// an attribute path naming this node becomes one naming none, so a change
// notification (which names none) marks it dirty, and an event path naming
// another node is left out (review II, R2-P6-store-0506, N4-im2-2).
func TestReestablishedRecordAnswersForThisNodeOnly(t *testing.T) {
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, fab)
	acl := accessControlACLPath()
	acl.HasNode, acl.Node = true, resumeDeviceNode
	rec := subscription.PeerSubscription{
		SubscriptionID: 0x5150, FabricIndex: resumeFabricIndex, PeerNodeID: resumeCtrlNode,
		AttributeRequests: []im.ConcreteAttributePath{acl},
		EventRequests: []im.ConcreteEventPath{{
			HasNode: true, Node: resumeDeviceNode + 1,
			HasEndpoint: true, Endpoint: 0, HasCluster: true, Cluster: 0x28,
		}},
		IsFabricFiltered: true, MinIntervalFloor: 0, MaxIntervalCeiling: 120, MaxInterval: 120, SendInterval: 60 * time.Second,
	}
	payload, err := subscription.MarshalPeerSubscription(rec)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.SaveServerSubscription(context.Background(), rec.SubscriptionID, resumeFabricIndex, resumeCtrlNode, payload)

	dev := startResumeDevice(t, st, func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: ctrl.addr})
	})
	ctrl.setBridge(dev.addr(t))
	res := dev.br.ReestablishFormerSubscriptions(context.Background())
	if !slices.Equal(res.Reestablished, []uint32{rec.SubscriptionID}) {
		t.Fatalf("ReestablishFormerSubscriptions = %+v", res)
	}
	ctrl.awaitStatusAck(t)
	if priming := ctrl.nextReport(t, time.Second); priming.subID != rec.SubscriptionID {
		t.Fatalf("priming = %+v", priming)
	}
	sub, err := dev.subs.Get(rec.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []im.ConcreteAttributePath{accessControlACLPath()}; !slices.Equal(sub.AttributePaths, want) {
		t.Fatalf("restored attribute paths = %+v, want %+v (the node cleared)", sub.AttributePaths, want)
	}
	if len(sub.EventPaths) != 0 {
		t.Fatalf("restored event paths = %+v, want none (another node's path left out)", sub.EventPaths)
	}
	dev.subs.OnAttributeChanged(accessControlACLPath())
	if ongoing := ctrl.nextReport(t, 2*time.Second); ongoing.subID != rec.SubscriptionID {
		t.Fatalf("ongoing report = %+v, want subscription %#x", ongoing, rec.SubscriptionID)
	}
}
