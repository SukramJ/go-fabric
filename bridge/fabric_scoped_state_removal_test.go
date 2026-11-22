// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/contract"
)

// fabricStateServer is a cluster server that holds state by fabric and
// records the fabrics it was told to drop.
type fabricStateServer struct {
	*notifyingServer
	mu      sync.Mutex
	dropped []uint8
}

var _ contract.FabricScopedState = (*fabricStateServer)(nil)

func (s *fabricStateServer) MatterDropFabric(_ context.Context, fabricIndex uint8) error {
	s.mu.Lock()
	s.dropped = append(s.dropped, fabricIndex)
	s.mu.Unlock()
	return nil
}

func (s *fabricStateServer) MatterHeldFabrics() []uint8 { return nil }

// fabricStateSource is an endpoint source whose one server holds state by
// fabric.
type fabricStateSource struct{ srv *fabricStateServer }

func (fabricStateSource) MatterDeviceType() uint16 { return 0x0073 }

func (s fabricStateSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.srv}
}

// TestFabricRemovalDropsFabricScopedState (round 2 of R2-P5-apidev-09): a
// host that runs the bridge without the node still has a device server's
// state of a removed fabric dropped — the bridge's fabric removal calls
// contract.FabricScopedState on the bridged endpoints' servers, the path
// the thermostat's open atomic writes take since its bridge-private
// capability went.
func TestFabricRemovalDropsFabricScopedState(t *testing.T) {
	t.Parallel()
	srv := &fabricStateServer{notifyingServer: newNotifyingServer(0x0201)}
	h, _ := newSourceHarness(t, fabricStateSource{srv: srv})
	h.bridge.EmitFabricRemoved(3)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !slices.Equal(srv.dropped, []uint8{3}) {
		t.Errorf("dropped %v after the removal of fabric 3, want [3]", srv.dropped)
	}
}
