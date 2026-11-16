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
	"github.com/SukramJ/go-fabric/store/memstore"
	"github.com/SukramJ/go-fabric/tlv"
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

// TestReestablishedPrimingSuppressesEventPathStatuses: the re-established
// priming report suppresses a concrete event path's status as it
// suppresses an attribute path's — the subscription is simulated as one
// that never went away. matter.js suppresses both with one flag
// (packages/node/src/node/server/ServerSubscription.ts:681-684 beside
// :647-650 at db021da0; review II, R2-P6-wire-0510, N-rw3req-2).
func TestReestablishedPrimingSuppressesEventPathStatuses(t *testing.T) {
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, fab)
	rec := subscription.PeerSubscription{
		SubscriptionID: 0x5151, FabricIndex: resumeFabricIndex, PeerNodeID: resumeCtrlNode,
		AttributeRequests: []im.ConcreteAttributePath{
			accessControlACLPath(),
			{HasEndpoint: true, Endpoint: 9, HasCluster: true, Cluster: 0x6, HasAttribute: true, Attribute: 0},
		},
		// An endpoint the node does not serve: UNSUPPORTED_ENDPOINT on a
		// fresh subscription.
		EventRequests: []im.ConcreteEventPath{{
			HasEndpoint: true, Endpoint: 9, HasCluster: true, Cluster: 0x28, HasEvent: true, Event: 0,
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
	priming := ctrl.nextReport(t, time.Second)
	rd, err := im.UnmarshalReportDataTLV(tlv.NewDecoder(priming.payload), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range rd.Reports {
		if a.IsStatus {
			t.Errorf("re-established priming carries the attribute status %+v", a)
		}
	}
	for _, e := range rd.EventReports {
		if e.IsStatus {
			t.Errorf("re-established priming carries the event status %+v", e)
		}
	}
	if len(rd.Reports) == 0 {
		t.Fatal("re-established priming carries no attribute data")
	}
}

// TestReestablishStopsAtInvalidSubscription (review II, R2-P6-fsm-0807):
// a peer that answers its newest former subscription's priming report
// InvalidSubscription has dropped its state for this node, so its older
// ones are not attempted; any other error status goes on with the next.
func TestReestablishStopsAtInvalidSubscription(t *testing.T) {
	for _, tc := range []struct {
		answer im.StatusCode
		want   []uint32
	}{
		{im.StatusInvalidSubscription, []uint32{0x702}},
		{im.StatusBusy, []uint32{0x702, 0x701}},
	} {
		fab := newResumeFabric(t)
		st := memstore.New()
		ctrl := newFakeController(t, fab)
		for _, id := range []uint32{0x701, 0x702} { // the older first
			payload, err := subscription.MarshalPeerSubscription(subscription.PeerSubscription{
				SubscriptionID: id, FabricIndex: resumeFabricIndex, PeerNodeID: resumeCtrlNode,
				AttributeRequests: []im.ConcreteAttributePath{accessControlACLPath()},
				IsFabricFiltered:  true, MaxIntervalCeiling: 120, MaxInterval: 120, SendInterval: 60 * time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			_ = st.SaveServerSubscription(context.Background(), id, resumeFabricIndex, resumeCtrlNode, payload)
		}
		ctrl.mu.Lock()
		ctrl.answer = tc.answer
		ctrl.mu.Unlock()
		dev := startResumeDevice(t, st, func(br *Bridge, sessions *operational.Manager) {
			br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
			br.AttachOperationalResolver(staticResolver{addr: ctrl.addr})
		})
		ctrl.setBridge(dev.addr(t))
		if res := dev.br.ReestablishFormerSubscriptions(context.Background()); len(res.Reestablished) != 0 {
			t.Fatalf("%v: re-established %v", tc.answer, res.Reestablished)
		}
		var tried []uint32
		for done := false; !done; {
			select {
			case r := <-ctrl.reports:
				tried = append(tried, r.subID)
			default:
				done = true
			}
		}
		if !slices.Equal(tried, tc.want) {
			t.Errorf("answered %v: priming reports for %#x, want %#x", tc.answer, tried, tc.want)
		}
	}
}
