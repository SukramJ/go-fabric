// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/store"
)

// ownNodeSetting is the own-node record a database of the releases before
// phase 37 holds (ota_provider.go ownNodeKey).
func ownNodeSetting(t *testing.T, st *store.Store, nodeID uint64) {
	t.Helper()
	if err := st.SetSetting(t.Context(), "controller.ownnode", fmt.Sprintf(`{"nodeId":%d}`, nodeID)); err != nil {
		t.Fatal(err)
	}
}

// TestOwnNodeRetiredWithoutOTA (review II, R2-P1-ctl-03): a database that
// ran the own node is migrated on its next start whether or not the
// controller serves OTA — the node goes from the registry and the record
// from the settings. Before, only a controller with Config.OTA migrated,
// and one without kept the vanished node for good.
func TestOwnNodeRetiredWithoutOTA(t *testing.T) {
	t.Parallel()
	var cfg controller.Config
	site, _, c, n := sitePair(t, fabrictest.Topology{}, func(c *controller.Config) { cfg = *c })
	addr, ok := n.PeerAddress()
	if !ok {
		t.Fatal("the paired node has no address")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if err := site.Run(ctx, c.Close); err != nil {
		t.Fatal(err)
	}
	st := store.New(cfg.DB)
	ownNodeSetting(t, st, addr.NodeID)

	again := cfg
	again.OTA = nil
	var c2 *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) { c2, err = controller.New(ctx, again); return err }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = site.Run(context.WithoutCancel(ctx), c2.Close) })
	if got := c2.Registry().GetByAddress(addr); got != nil {
		t.Fatalf("the former own node %s is still in the registry", got.ID())
	}
	if _, ok, err := st.GetSetting(ctx, "controller.ownnode"); err != nil || ok {
		t.Fatalf("the own-node record after the start: ok=%v err=%v, want gone", ok, err)
	}
}
