// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// TestFactoryResetThatDidNotStopTheBridgeErasesNothing (review II,
// R2-P1-rw-03): when the stop inside FactoryReset gives up — its context
// ends while a bridge Start the host runs holds the lifecycle — the node
// is not stopped, its bridge still serves, and the reset neither erases
// nor closes the store under it: it returns the stop's error, and a later
// FactoryReset with a live context finishes the work. Before, the store
// was erased and closed under the serving bridge.
func TestFactoryResetThatDidNotStopTheBridgeErasesNothing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "n.db")
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Path: path}
	cfg.Network.Listen = freeUDPAddr(t)
	n := newNode(t, cfg)
	ctx, cancelCtx := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelCtx()
	if err := n.Store().SetSetting(ctx, "bs.secret", "kept"); err != nil {
		t.Fatal(err)
	}
	g := &gatedPlug{plug: newPlug(t, "gated"), armed: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{})}
	if err := n.Aggregator().Add(ctx, g); err != nil {
		t.Fatal(err)
	}
	close(g.armed)
	bridgeStarted := make(chan error, 1)
	go func() { bridgeStarted <- n.Bridge().Start(ctx) }()
	select {
	case <-g.entered:
	case <-ctx.Done():
		t.Fatal("the bridge Start never reached the snapshot")
	}

	done, cancel := context.WithCancel(t.Context())
	cancel()
	if err := n.FactoryReset(done); err == nil {
		close(g.release)
		<-bridgeStarted
		t.Fatal("FactoryReset with a done context returned nil while a bridge Start held the lifecycle")
	}
	if v, ok, err := n.Store().GetSetting(ctx, "bs.secret"); err != nil || !ok || v != "kept" {
		t.Errorf("the store after a FactoryReset whose stop did not complete: %q %v %v, want the setting kept in an open store", v, ok, err)
	}
	close(g.release)
	if err := <-bridgeStarted; err != nil {
		t.Fatalf("the bridge Start: %v", err)
	}
	if err := n.FactoryReset(ctx); err != nil {
		t.Fatalf("the second FactoryReset: %v", err)
	}
	if got := n.LocalAddr(); got != "" {
		t.Fatalf("the bridge still bound to %s after the second FactoryReset", got)
	}
	if v, ok, err := store.New(openDB(t, path)).GetSetting(ctx, "bs.secret"); err != nil || ok {
		t.Errorf("the store after the second FactoryReset: %q %v %v, want the setting erased", v, ok, err)
	}
}
