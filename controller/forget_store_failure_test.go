// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/store"
)

// refuseErase makes the client store's erase of a node fail — every
// DELETE of a stored data version aborts — until the returned function
// lifts it.
func refuseErase(t *testing.T, db *sql.DB) (lift func()) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER refuse_erase BEFORE DELETE ON matter_client_versions BEGIN SELECT RAISE(ABORT, 'erase refused'); END`); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := db.ExecContext(context.WithoutCancel(t.Context()), `DROP TRIGGER refuse_erase`); err != nil {
			t.Fatal(err)
		}
	}
}

// TestForgetReportsAStoreThatKeptTheNode (review II): a Forget whose
// erase of the node's rows fails returns the store's error and keeps the
// node in the registry, so a later Forget retries it. Before, the failure
// was logged and Forget succeeded with the node's rows still stored: the
// node came back at the next start.
func TestForgetReportsAStoreThatKeptTheNode(t *testing.T) {
	t.Parallel()
	var cfg controller.Config
	site, _, c, n := sitePair(t, fabrictest.Topology{}, func(c *controller.Config) { cfg = *c })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	lift := refuseErase(t, cfg.DB)
	if err := site.Run(ctx, func(ctx context.Context) error { return c.Registry().Forget(ctx, n) }); err == nil {
		t.Fatal("Forget succeeded with the node's rows still stored")
	}
	if got := c.Registry().Get(n.ID()); got != n {
		t.Fatalf("after the failed Forget the registry holds %v, want the node", got)
	}
	lift()
	if err := site.Run(ctx, func(ctx context.Context) error { return c.Registry().Forget(ctx, n) }); err != nil {
		t.Fatalf("Forget again: %v", err)
	}
	if got := c.Registry().Get(n.ID()); got != nil {
		t.Fatal("the forgotten node is still in the registry")
	}
	if err := site.Run(ctx, c.Close); err != nil {
		t.Fatal(err)
	}
}

// TestOwnNodeRetiredAgainAfterAFailedForget (review II, R2-P1-ctl-03 part
// b): a start whose Forget of the former own node fails keeps the
// own-node record, and the controller starts all the same; the next start
// retires the node. Before, the record was deleted after the failure was
// logged — Forget reported success with the rows still stored — and the
// node stayed for good.
func TestOwnNodeRetiredAgainAfterAFailedForget(t *testing.T) {
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
	start := func() *controller.Controller {
		t.Helper()
		var c *controller.Controller
		if err := site.Run(ctx, func(ctx context.Context) (err error) { c, err = controller.New(ctx, cfg); return err }); err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	}
	lift := refuseErase(t, cfg.DB)
	c2 := start()
	if _, ok, err := st.GetSetting(ctx, "controller.ownnode"); err != nil || !ok {
		t.Fatalf("the own-node record after a failed Forget: ok=%v err=%v, want kept", ok, err)
	}
	if err := site.Run(ctx, c2.Close); err != nil {
		t.Fatal(err)
	}
	lift()
	c3 := start()
	t.Cleanup(func() { _ = site.Run(context.WithoutCancel(ctx), c3.Close) })
	if got := c3.Registry().GetByAddress(addr); got != nil {
		t.Fatalf("the former own node %s is still in the registry", got.ID())
	}
	if _, ok, err := st.GetSetting(ctx, "controller.ownnode"); err != nil || ok {
		t.Fatalf("the own-node record after the retry: ok=%v err=%v, want gone", ok, err)
	}
}
