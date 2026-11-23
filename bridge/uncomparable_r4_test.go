// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
)

// ifaceNotifier is a notifier and quiescing server whose type is
// comparable but whose value is not when meta holds a slice: == on it, or
// a map key, panics (review II, R4-api-4753, decision 618).
type ifaceNotifier struct {
	meta any
}

func (ifaceNotifier) OnMatterValueChanged(func()) func() { return func() {} }

// TestSameNotifierJudgesTheValue (review II, R4-api-4753, decision 618):
// a notifier of a comparable type whose value cannot be compared is never
// the same as another, and judging it does not panic.
func TestSameNotifierJudgesTheValue(t *testing.T) {
	t.Parallel()
	a := ifaceNotifier{meta: []string{"host"}}
	var same bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("sameNotifier panicked: %v", r)
			}
		}()
		same = sameNotifier(a, a)
	}()
	if same {
		t.Error("a notifier that cannot be compared counted as the same")
	}
	if !sameNotifier(ifaceNotifier{meta: 1}, ifaceNotifier{meta: 1}) {
		t.Error("two equal comparable notifiers are not the same")
	}
}

// TestReconciledLeavesOutAValueThatCannotBeCompared (review II,
// R4-api-4753, decision 618): reconciled lists no server whose value
// cannot be a map key, so reconcileQuiescers does not panic on it.
func TestReconciledLeavesOutAValueThatCannotBeCompared(t *testing.T) {
	t.Parallel()
	root := &endpoint.Endpoint{ID: 0}
	root.PublishClusterServers([]contract.ClusterServer{
		quiescingServer{ifaceNotifier: ifaceNotifier{meta: []string{"host"}}},
		quiescingServer{ifaceNotifier: ifaceNotifier{meta: 7}},
	})
	topo := &endpoint.Topology{Endpoints: []*endpoint.Endpoint{root}}
	var got []reconciledServer
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("reconciled panicked: %v", r)
			}
		}()
		got = reconciled(topo)
		seen := map[contract.ClusterServer]bool{}
		for _, r := range got {
			seen[r.server] = true
		}
	}()
	if len(got) != 1 {
		t.Errorf("reconciled listed %d servers, want only the comparable one", len(got))
	}
}

// quiescingServer is a [contract.ClusterServer] (whose methods are not
// called here) and [contract.ClusterQuiescer] around an ifaceNotifier.
type quiescingServer struct {
	contract.ClusterServer
	ifaceNotifier
}

func (quiescingServer) MatterQuiesce() {}
