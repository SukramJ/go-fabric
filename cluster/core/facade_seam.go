// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"

	"github.com/SukramJ/go-fabric/internal/facadeseam"
)

func init() {
	facadeseam.SetOnCommissioningComplete = func(server any, fn func(context.Context, uint8)) bool {
		g, ok := server.(*GeneralCommissioning)
		if !ok || g == nil {
			return false
		}
		g.mu.Lock()
		g.facadeOnCommissioningComplete = fn
		g.mu.Unlock()
		return true
	}
	facadeseam.SetOnFabricRemoved = func(server any, fn func(context.Context, uint8)) bool {
		o, ok := server.(*OperationalCredentials)
		if !ok || o == nil {
			return false
		}
		o.mu.Lock()
		o.facadeOnFabricRemoved = fn
		o.mu.Unlock()
		return true
	}
	facadeseam.SetOnFabricUpdated = func(server any, fn func(context.Context, uint8)) bool {
		o, ok := server.(*OperationalCredentials)
		if !ok || o == nil {
			return false
		}
		o.mu.Lock()
		o.facadeOnFabricUpdated = fn
		o.mu.Unlock()
		return true
	}
	facadeseam.WithACLWriteLock = func(server any, fn func()) bool {
		a, ok := server.(*AccessControl)
		if !ok || a == nil {
			return false
		}
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
		fn()
		return true
	}
}

// chainFabricHooks is the facade's slot, then the host's, as one hook; nil
// when neither is set (internal/facadeseam).
func chainFabricHooks(facade, host func(context.Context, uint8)) func(context.Context, uint8) {
	switch {
	case facade == nil:
		return host
	case host == nil:
		return facade
	}
	return func(ctx context.Context, fabricIndex uint8) {
		facade(ctx, fabricIndex)
		host(ctx, fabricIndex)
	}
}
