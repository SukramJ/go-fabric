// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/node"
)

// TestSetCommissioningBlockedDuringStart pins ADR 0026 P6: a host's call
// path never waits on a lock the node holds across host code. The host's
// Snapshotter runs inside Start, which holds the lifecycle lock; a
// SetCommissioningBlocked from it returns, and Start completes.
func TestSetCommissioningBlockedDuringStart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	if err := sqlitestore.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := minimalConfig(db)
	asm, err := endpoint.New(sqlitestore.New(db), endpoint.Config{
		VendorID: cfg.ProductDescription.VendorID, ProductID: cfg.ProductDescription.ProductID,
		NodeLabel: cfg.BasicInformation.NodeLabel,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var (
		n      atomic.Pointer[node.Node]
		called atomic.Bool
	)
	cfg.Topology = func(ctx context.Context) (*endpoint.Topology, error) {
		if nn := n.Load(); nn != nil && called.CompareAndSwap(false, true) {
			nn.SetCommissioningBlocked(ctx, nil)
		}
		return asm.Assemble(ctx, nil)
	}
	n.Store(newNode(t, cfg))
	started := make(chan error, 1)
	go func() { started <- n.Load().Start(ctx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("SetCommissioningBlocked from the Snapshotter waited for Start's lifecycle lock")
	}
	if !called.Load() {
		t.Fatal("the Snapshotter was not called during Start")
	}
}
