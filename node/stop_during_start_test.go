// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/node"
)

// TestStopDuringStartIsBoundedByItsContext (review II, R2-P2-stop-08): a
// Stop that arrives while Start runs — here held in the host's
// Snapshotter — waits for it only until its own ctx ends, and then returns
// ctx's error with nothing torn down; a later Stop stops the node. Before,
// Stop waited for Start whatever its ctx.
func TestStopDuringStartIsBoundedByItsContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	if err := sqlitestore.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := minimalConfig(db)
	asm, err := endpoint.New(sqlitestore.New(db), endpoint.Config{
		VendorID: cfg.ProductDescription.VendorID, ProductID: cfg.ProductDescription.ProductID,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var held atomic.Bool
	cfg.Snapshotter = func(ctx context.Context) (*endpoint.Topology, error) {
		if held.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		return asm.Assemble(ctx, nil)
	}
	n := newNode(t, cfg)
	started := make(chan error, 1)
	go func() { started <- n.Start(ctx) }()
	<-entered

	gone, cancel := context.WithCancel(ctx)
	cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- n.Stop(gone) }()
	bound, boundCancel := context.WithTimeout(t.Context(), 10*time.Second) // only names the failure: a Stop that waits for Start never returns here
	defer boundCancel()
	select {
	case err := <-stopped:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Stop with a done ctx during Start = %v, want context.Canceled", err)
		}
	case <-bound.Done():
		t.Error("Stop with a done ctx waited for the Start in progress")
	}
	close(release)
	if err := <-started; err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n.LocalAddr() == "" {
		t.Error("the Start the refused Stop waited for did not complete")
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatalf("Stop after Start: %v", err)
	}
	if err := n.Start(ctx); !errors.Is(err, node.ErrStopped) {
		t.Errorf("Start after Stop = %v, want ErrStopped", err)
	}
}
