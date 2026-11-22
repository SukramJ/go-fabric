// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster"
	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/tlv"
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

// TestDeviceLoadCountsPerFabric holds CurrentSubscriptionsForFabric
// (R2-P6-test-0816): it counts the subscriptions whose session resolves to
// the accessing fabric, CurrentSubscriptions counts all of them, and a read
// without an accessing fabric (index 0) counts none for the fabric.
func TestDeviceLoadCountsPerFabric(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	b.sessions = sessionTable{7: {1, 0x1111}, 8: {2, 0x2222}}
	mgr := subscription.NewManager(subscription.Config{}, func(context.Context, *subscription.Subscription, []im.ConcreteAttributePath) {}, nil)
	for _, s := range []struct {
		session uint16
		fabric  uint8
		peer    uint64
	}{{7, 1, 0x1111}, {7, 1, 0x1111}, {8, 2, 0x2222}} {
		if _, err := mgr.Subscribe(subscription.SubscribeArgs{
			FabricIndex: s.fabric, PeerNodeID: s.peer, SessionID: s.session, MaxIntervalCeiling: 60,
			AttributePaths: []im.ConcreteAttributePath{{Endpoint: 0, Cluster: 0x28, Attribute: 5, HasEndpoint: true, HasCluster: true, HasAttribute: true}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	b.subManager = mgr
	for _, c := range []struct {
		fabric    uint8
		forFabric uint16
	}{{1, 2}, {2, 1}, {3, 0}, {0, 0}} {
		got := b.DeviceLoad(c.fabric)
		if got.CurrentSubscriptions != 3 || got.CurrentSubscriptionsForFabric != c.forFabric {
			t.Errorf("fabric %d: current %d, for fabric %d; want 3, %d", c.fabric, got.CurrentSubscriptions, got.CurrentSubscriptionsForFabric, c.forFabric)
		}
	}
}

// TestDeviceLoadClamps holds the saturation of DeviceLoadStatus's counts at
// their fields' maxima (R2-P6-test-0816): a count past the maximum reads as
// the maximum, not as its truncated low bits.
func TestDeviceLoadClamps(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		n    int
		want uint16
	}{{0, 0}, {0xFFFE, 0xFFFE}, {0xFFFF, 0xFFFF}, {0x1_0000, 0xFFFF}, {0x1_0001, 0xFFFF}} {
		if got := clampUint16(c.n); got != c.want {
			t.Errorf("clampUint16(%#x) = %#x, want %#x", c.n, got, c.want)
		}
	}
	for _, c := range []struct {
		n    uint64
		want uint32
	}{{0, 0}, {0xFFFF_FFFE, 0xFFFF_FFFE}, {0xFFFF_FFFF, 0xFFFF_FFFF}, {1 << 32, 0xFFFF_FFFF}, {1<<32 + 1, 0xFFFF_FFFF}} {
		if got := clampUint32(c.n); got != c.want {
			t.Errorf("clampUint32(%#x) = %#x, want %#x", c.n, got, c.want)
		}
	}
	b := &Bridge{}
	b.load.imSent.Add(1 << 32)
	if got := b.DeviceLoad(1).TotalInteractionModelMessagesSent; got != 0xFFFF_FFFF {
		t.Errorf("TotalInteractionModelMessagesSent at 2^32 = %#x, want the maximum", got)
	}
}
