// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestDeviceLoadStatusIsWiredAndCounts pins GeneralDiagnostics
// DeviceLoadStatus (0x000A, conformance "Rev >= v3"): attaching the cluster
// to the root wires the bridge's counters, which count IM messages and
// established subscriptions, and the current subscriptions per fabric.
// Mirrors matter.js GeneralDiagnosticsServer.ts deviceLoadStatus. Found
// missing by the CHIP conformance checker (TC-IDM-10.2).
func TestDeviceLoadStatusIsWiredAndCounts(t *testing.T) {
	t.Parallel()
	b := newReachabilityBridge(t, nil)
	gd := mattercore.NewGeneralDiagnostics(mattercore.BootReasonPowerOnReboot)
	b.AttachRootClusters([]contract.ClusterServer{gd})

	if attrs := gd.MatterAttributes(); !contains(attrs, 0x000A) {
		t.Fatalf("AttributeList %v lacks DeviceLoadStatus", attrs)
	}
	b.load.imReceived.Add(5)
	b.load.imSent.Add(3)
	b.load.subscriptionsSucceeded.Add(2)
	mgr := subscription.NewManager(subscription.Config{}, func(context.Context, *subscription.Subscription, []im.ConcreteAttributePath) {}, nil)
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		PeerNodeID: 1, SessionID: 9, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{{Endpoint: 0, Cluster: 0x28, Attribute: 5, HasEndpoint: true, HasCluster: true, HasAttribute: true}},
	}); err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(mgr)

	v, ok := gd.MatterReadFiltered(im.WithFabricFilter(context.Background(), false, 3), 0x000A)
	if !ok {
		t.Fatal("DeviceLoadStatus not served")
	}
	got := v.(mattercore.DeviceLoadStruct)
	want := mattercore.DeviceLoadStruct{
		CurrentSubscriptions: 1, TotalSubscriptionsEstablished: 2,
		TotalInteractionModelMessagesSent: 3, TotalInteractionModelMessagesReceived: 5,
	}
	if got != want {
		t.Errorf("DeviceLoadStatus = %+v, want %+v (session 9 belongs to no fabric here)", got, want)
	}

	enc := tlv.NewEncoder()
	defaultAttributeValueWriter(enc, tlv.AnonymousTag(), im.AttributeValue{Value: got})
	raw, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(raw)
	if _, err := dec.Next(); err != nil {
		t.Fatal(err)
	}
	m, err := decodeGenericTagMap(dec)
	if err != nil {
		t.Fatal(err)
	}
	if m[0] != uint64(1) || m[2] != uint64(2) || m[3] != uint64(3) || m[4] != uint64(5) {
		t.Errorf("wire struct %v", m)
	}
	if fm, _ := gd.MatterRead(cluster.AttrGlobalFeatureMap); fm == nil {
		t.Error("FeatureMap missing")
	}
}

func contains(ids []uint32, id uint32) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
