// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/store"
)

// counterServer is a root cluster server without a DataVersion tracker of
// its own whose command 0 moves attribute 0.
type counterServer struct{ n atomic.Uint32 }

func (*counterServer) MatterClusterID() uint32 { return 0xFFF1_FC02 }

func (s *counterServer) MatterRead(attrID uint32) (any, bool) {
	if attrID != 0 {
		return nil, false
	}
	return s.n.Load(), true
}

func (*counterServer) MatterWrite(context.Context, uint32, any) error { return nil }

func (s *counterServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	s.n.Add(1)
	return nil, nil
}

func (*counterServer) MatterReportable() []uint32        { return []uint32{0} }
func (*counterServer) MatterAcceptedCommands() []uint32  { return []uint32{0} }
func (*counterServer) MatterGeneratedCommands() []uint32 { return nil }

var _ contract.ClusterCommandLister = (*counterServer)(nil)

// TestRootInvokeChangeAdvancesTheEndpointHeldVersion (review II, decision
// 46, R2-P2-ctr-05): a command that moves an attribute of a root server
// without a tracker of its own — TimeSynchronization's SetUTCTime is one —
// advances the root's endpoint-held DataVersion, so the version moves with
// the data. Before, a root server's version came from its own tracker
// alone, and such a server reported the constant 1.
func TestRootInvokeChangeAdvancesTheEndpointHeldVersion(t *testing.T) {
	t.Parallel()
	srv := &counterServer{}
	h := newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
		return nil, []contract.ClusterServer{srv}
	})
	h.allowAll()
	h.bridge.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, (&reachAttrReporterSpy{}).report, nil))
	root := h.bridge.Topology().FindByID(0)
	before := root.ClusterDataVersion(srv.MatterClusterID())
	h.invoke(0, srv.MatterClusterID(), 0, nil)
	if srv.n.Load() != 1 {
		t.Fatalf("the command ran %d times, want 1", srv.n.Load())
	}
	if after := root.ClusterDataVersion(srv.MatterClusterID()); after == before {
		t.Fatalf("root DataVersion %d unchanged after a command moved its data", after)
	}
}
