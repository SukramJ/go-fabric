// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import "thingfabric.dev/go-fabric/internal/facadeseam"

func init() {
	facadeseam.SetTransitionHook = func(window any, fn func()) bool {
		w, ok := window.(*CommissioningWindow)
		if !ok || w == nil {
			return false
		}
		w.mu.Lock()
		w.facadeOnTransition = fn
		w.mu.Unlock()
		return true
	}
	facadeseam.OwnFabricState = func(bridge any) bool {
		b, ok := bridge.(*Bridge)
		if !ok || b == nil {
			return false
		}
		b.fabricStateByNode.Store(true)
		return true
	}
}
