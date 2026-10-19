// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import "testing"

// TestEventLogDropFabric: the fabric-sensitive records of the removed
// fabric leave the log; another fabric's, and a record of an event that is
// not fabric-sensitive whatever its payload, stay.
func TestEventLogDropFabric(t *testing.T) {
	t.Parallel()
	type payload struct{ FabricIndex uint8 }
	l := NewEventLog()
	l.Append(EventRecord{Cluster: matterAccessControlClusterID, Payload: payload{1}})
	l.Append(EventRecord{Cluster: matterAccessControlClusterID, Payload: payload{2}})
	l.Append(EventRecord{Cluster: matterMessagesClusterID, Payload: &payload{1}})
	l.Append(EventRecord{Cluster: 0x0028, EventID: 2, Payload: payload{1}}) // BasicInformation Leave
	if n := l.DropFabric(1); n != 2 {
		t.Fatalf("dropped %d, want 2", n)
	}
	got := l.Query(0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0)
	if len(got) != 2 || got[0].Cluster != matterAccessControlClusterID || got[1].Cluster != 0x0028 {
		t.Fatalf("left %+v", got)
	}
	if n := l.DropFabric(9); n != 0 {
		t.Fatalf("dropped %d for an unknown fabric", n)
	}
}
