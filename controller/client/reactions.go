// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import (
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
)

// onEvent is the registry's reaction to a node's event: the BasicInformation
// events of the root (Peers.ts:714-729 at 0ceb202d).
func (r *Registry) onEvent(n *Node, endpoint uint16, cluster, event uint32, value any) {
	if endpoint != 0 || cluster != basicinformation.ClusterID {
		return
	}
	if event == basicinformation.EventLeave {
		if idx, ok := leaveFabricIndex(value); ok {
			r.noteLeave(n, idx)
		}
	}
}

// leaveFabricIndex is the fabric index of a Leave event's payload.
func leaveFabricIndex(value any) (uint8, bool) {
	switch v := value.(type) {
	case basicinformation.LeaveEvent:
		return v.FabricIndex, true
	case *basicinformation.LeaveEvent:
		return v.FabricIndex, v != nil
	}
	return 0, false
}
