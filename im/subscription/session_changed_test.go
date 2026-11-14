// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

// TestSessionSubscriptionsChangedHook: the hook fires with the session id
// when a subscription is added on it and again when it is closed on it,
// whichever close path; nil detaches it (matter.js SessionManager
// subscriptionsChanged, SessionsBehavior.ts:53, :75-93 at 862ad75f).
func TestSessionSubscriptionsChangedHook(t *testing.T) {
	t.Parallel()
	m := NewManager(Config{}, nil, nil)
	var seen []uint16
	m.SetOnSessionSubscriptionsChanged(func(id uint16) { seen = append(seen, id) })
	sub, err := m.Subscribe(SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 0xC0, SessionID: 5, MinIntervalFloor: 1, MaxIntervalCeiling: 10,
		AttributePaths: []im.ConcreteAttributePath{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.CloseSession(sub.SessionID)
	if want := []uint16{5, 5}; !slices.Equal(seen, want) {
		t.Fatalf("hook saw %v, want %v (added, closed)", seen, want)
	}
	m.SetOnSessionSubscriptionsChanged(nil)
	if _, err := m.Subscribe(SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 0xC0, SessionID: 6, MinIntervalFloor: 1, MaxIntervalCeiling: 10,
		AttributePaths: []im.ConcreteAttributePath{{}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Errorf("a detached hook fired: %v", seen)
	}
}

// TestSessionSubscriptionsChangedHookOnRestore: a former subscription
// restored on a session after a restart is a subscription added on it —
// the hook fires for it as for one Subscribe admits (review II,
// R2-P2-sess-08).
func TestSessionSubscriptionsChangedHookOnRestore(t *testing.T) {
	t.Parallel()
	m := NewManager(Config{MaxIntervalCeilingSeconds: 60}, nil, nil)
	var seen []uint16
	m.SetOnSessionSubscriptionsChanged(func(id uint16) { seen = append(seen, id) })
	if _, err := m.Restore(samplePeerSubscription(), 0x4242); err != nil {
		t.Fatal(err)
	}
	if want := []uint16{0x4242}; !slices.Equal(seen, want) {
		t.Fatalf("hook saw %v after Restore, want %v", seen, want)
	}
}
