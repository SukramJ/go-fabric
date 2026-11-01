// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

// TestMarkDirtyAfterCloseIsIgnored: a change that reaches a subscription
// after Close records nothing — the engine's tick must find no pending
// path on a closed subscription. The bridge's fan-out and a Close race in
// production, which is why the branch is otherwise only reached by
// timing; this pins it deterministically.
func TestMarkDirtyAfterCloseIsIgnored(t *testing.T) {
	t.Parallel()
	path := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 1, Cluster: 0x0006, Attribute: 0x0000}
	m := NewManager(Config{}, nil, nil)
	sub, err := m.Subscribe(SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 1, SessionID: 1,
		MinIntervalFloor: 0, MaxIntervalCeiling: 3600,
		AttributePaths: []im.ConcreteAttributePath{path},
	})
	if err != nil {
		t.Fatal(err)
	}
	sub.Close()
	sub.markDirty(path)
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if len(sub.pendingDirty) != 0 {
		t.Fatalf("pendingDirty %v after Close, want none", sub.pendingDirty)
	}
}
