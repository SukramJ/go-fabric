// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"sync/atomic"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
)

// deviceLoadCounters are the node-wide Interaction Model counters
// GeneralDiagnostics' DeviceLoadStatus (Matter 1.6, cluster revision 3)
// reports. matter.js keeps the same three in its InteractionServer
// (counters.totalSubscriptionsEstablished,
// totalInteractionModelMessagesSent / Received) and reads the current
// subscriptions off its SessionManager (GeneralDiagnosticsServer.ts
// deviceLoadStatus).
type deviceLoadCounters struct {
	imReceived             atomic.Uint64
	imSent                 atomic.Uint64
	subscriptionsSucceeded atomic.Uint64
}

// wireDeviceLoad hands every GeneralDiagnostics server among the root
// clusters this bridge's counters, so a host that mounts the cluster gets
// a live DeviceLoadStatus without wiring it. Called before the bridge lock
// is taken: the provider is read back under the server's own lock.
func (b *Bridge) wireDeviceLoad(servers []contract.ClusterServer) {
	for _, srv := range servers {
		if gd, ok := srv.(*mattercore.GeneralDiagnostics); ok {
			gd.SetDeviceLoadProvider(b.DeviceLoad)
		}
	}
}

// DeviceLoad reports the node's Interaction Model load for the accessing
// fabric, as DeviceLoadStatus carries it. Each count saturates at its
// field's maximum, as matter.js clamps them.
func (b *Bridge) DeviceLoad(fabricIndex uint8) mattercore.DeviceLoadStruct {
	var current, forFabric int
	if mgr := b.subscriptionManagerLocked(); mgr != nil {
		for sessionID, n := range mgr.CountBySession() {
			current += n
			if fabricIndex != 0 && b.resolveSessionFabric(sessionID) == fabricIndex {
				forFabric += n
			}
		}
	}
	return mattercore.DeviceLoadStruct{
		CurrentSubscriptions:                  clampUint16(current),
		CurrentSubscriptionsForFabric:         clampUint16(forFabric),
		TotalSubscriptionsEstablished:         clampUint32(b.load.subscriptionsSucceeded.Load()),
		TotalInteractionModelMessagesSent:     clampUint32(b.load.imSent.Load()),
		TotalInteractionModelMessagesReceived: clampUint32(b.load.imReceived.Load()),
	}
}

func clampUint16(n int) uint16 {
	if n > 0xFFFF {
		return 0xFFFF
	}
	return uint16(n) //nolint:gosec // clamped on the line above
}

func clampUint32(n uint64) uint32 {
	if n > 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(n)
}
