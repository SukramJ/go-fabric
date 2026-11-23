// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller/client"
)

// TestUnavailableNodeTakesItsHooksFromHooksFor holds R3-fsm-3801: a node
// that failed to load carries the hooks RegistryConfig.HooksFor returns
// for its id, as a node that loaded does — so a chain passed only as
// HooksFor (the controller's update manager hand-off) sees the node's
// Deleted when the registry forgets it. Before the fix the unavailable
// node was built from RegistryConfig.Node.Hooks alone and its Deleted
// never reached the HooksFor chain.
func TestUnavailableNodeTakesItsHooksFromHooksFor(t *testing.T) {
	t.Parallel()
	var (
		mu      sync.Mutex
		deleted []string
	)
	sr := newScriptedRegistry(t, func(st *client.MemoryStore) {
		seedCommissioned(1)(st)
		corruptCommissionedAt(t, st, "peer1")
	}, func(cfg *client.RegistryConfig) {
		cfg.HooksFor = func(id string) client.Hooks {
			return client.Hooks{Deleted: func() {
				mu.Lock()
				deleted = append(deleted, id)
				mu.Unlock()
			}}
		}
	})
	broken := sr.reg.Get("peer1")
	if broken == nil || !errors.Is(broken.LoadError(), client.ErrLoadFailed) {
		t.Fatalf("peer1 %v, want a node that failed to load", broken)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sr.reg.Forget(ctx, broken); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deleted) != 1 || deleted[0] != "peer1" {
		t.Fatalf("HooksFor's Deleted ran for %v, want once for peer1", deleted)
	}
}
