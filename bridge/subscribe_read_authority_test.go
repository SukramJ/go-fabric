// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
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
