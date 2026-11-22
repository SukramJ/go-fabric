// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// TestDeniedSubscriberDrivesNoServerRead (decision 162, the gate's
// hand-over to wave 3): the priming report and every report read under
// the subscriber's read check, so a subject without the grant on a
// cluster drives no read of its server — for a concrete and a wildcard
// path alike — while its granted path is reported.
func TestDeniedSubscriberDrivesNoServerRead(t *testing.T) {
	aclCluster := uint32(0x001F)
	srv := &countingOnOff{}
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, newResumeFabric(t))
	dev := startResumeDeviceWith(t, st, []store.ACLEntry{{
		FabricIndex: resumeFabricIndex, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{resumeCtrlNode}, Targets: []store.ACLTarget{{Cluster: &aclCluster}},
	}}, []contract.ClusterServer{srv}, nil)
	ctrl.setBridge(dev.addr(t))
	subID := subscribeOverCASEWith(t, dev, ctrl, im.SubscribeRequest{
		KeepSubscriptions: true, MaxIntervalCeiling: 120, FabricFiltered: true,
		AttributeRequests: []im.ConcreteAttributePath{
			accessControlACLPath(),
			rootOnOffPath(),
			{HasCluster: true, Cluster: 0x0006},
		},
	})
	if n := srv.reads.Load(); n != 0 {
		t.Fatalf("the priming report of a denied subject read the server %d times", n)
	}
	dev.subs.OnAttributeChanged(rootOnOffPath())
	dev.subs.OnAttributeChanged(accessControlACLPath())
	if r := ctrl.nextReport(t, 2*time.Second); r.subID != subID {
		t.Fatalf("report = %+v, want subscription %#x", r, subID)
	}
	if n := srv.reads.Load(); n != 0 {
		t.Fatalf("a report of a denied subject read the server %d times", n)
	}
}

// TestDeniedPathsAheadOfTheGrantedOneDriveNoServerRead (Fable's read of
// sub round 1, A-1): the check whether the subject still reads any of its
// paths walks them before a report, and it judges a location without
// reading the server there — with the denied paths ahead of the granted
// one, neither the walk of the first report nor that of a second
// subscription of the same subject reads the denied server.
func TestDeniedPathsAheadOfTheGrantedOneDriveNoServerRead(t *testing.T) {
	aclCluster := uint32(0x001F)
	srv := &countingOnOff{}
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, newResumeFabric(t))
	dev := startResumeDeviceWith(t, st, []store.ACLEntry{{
		FabricIndex: resumeFabricIndex, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{resumeCtrlNode}, Targets: []store.ACLTarget{{Cluster: &aclCluster}},
	}}, []contract.ClusterServer{srv}, nil)
	ctrl.setBridge(dev.addr(t))
	req := im.SubscribeRequest{
		KeepSubscriptions: true, MaxIntervalCeiling: 120, FabricFiltered: true,
		AttributeRequests: []im.ConcreteAttributePath{
			rootOnOffPath(),
			{HasCluster: true, Cluster: 0x0006},
			accessControlACLPath(),
		},
	}
	for i := range 2 {
		subID := subscribeOverCASEWith(t, dev, ctrl, req)
		dev.subs.OnAttributeChanged(rootOnOffPath())
		dev.subs.OnAttributeChanged(accessControlACLPath())
		if r := ctrl.nextReport(t, 2*time.Second); r.subID != subID {
			t.Fatalf("subscription %d: report = %+v, want subscription %#x", i+1, r, subID)
		}
		if n := srv.reads.Load(); n != 0 {
			t.Fatalf("subscription %d: a report of a denied subject read the server %d times", i+1, n)
		}
	}
}
