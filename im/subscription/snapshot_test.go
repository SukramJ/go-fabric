// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

// TestSnapshot: Snapshot lists the open subscriptions by id as copies —
// a closed one is left out, and a caller's edit of a listed path does not
// reach the subscription (brief M9-module §3 a).
func TestSnapshot(t *testing.T) {
	t.Parallel()
	path := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 1, Cluster: 0x0006, Attribute: 0x0000}
	event := im.ConcreteEventPath{HasEndpoint: true, HasCluster: true, HasEvent: true, Endpoint: 0, Cluster: 0x0028, Event: 0x00}
	m := NewManager(Config{}, nil, nil)
	var subs []*Subscription
	for i := range 3 {
		sub, err := m.Subscribe(SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 7, SessionID: uint16(10 + i),
			MinIntervalFloor: 1, MaxIntervalCeiling: 60, KeepSubscriptions: i == 0,
			AttributePaths: []im.ConcreteAttributePath{path}, EventPaths: []im.ConcreteEventPath{event},
		})
		if err != nil {
			t.Fatal(err)
		}
		subs = append(subs, sub)
	}
	subs[1].Close()
	got := m.Snapshot()
	if len(got) != 2 {
		t.Fatalf("Snapshot = %+v, want the two open subscriptions", got)
	}
	if got[0].ID >= got[1].ID {
		t.Errorf("not by id: %d, %d", got[0].ID, got[1].ID)
	}
	for _, info := range got {
		if info.ID == subs[1].ID {
			t.Errorf("the closed subscription %d is listed", info.ID)
		}
		if info.FabricIndex != 1 || info.PeerNodeID != 7 || info.MinIntervalFloor != 1 || info.MaxIntervalCeiling != 60 ||
			len(info.AttributePaths) != 1 || len(info.EventPaths) != 1 || info.SendInterval <= 0 {
			t.Errorf("info = %+v", info)
		}
		if info.ID == subs[0].ID && (!info.KeepSubscriptions || info.SessionID != 10) {
			t.Errorf("first subscription = %+v", info)
		}
	}
	got[0].AttributePaths[0].Cluster = 0
	for _, sub := range subs {
		if sub.AttributePaths[0].Cluster != 0x0006 {
			t.Fatal("the snapshot shares a subscription's paths")
		}
	}
}
