// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import "github.com/SukramJ/go-fabric/internal/facadeseam"

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
}
