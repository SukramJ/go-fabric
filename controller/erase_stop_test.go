// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestEraseWaitsForTheNodesStop (review II, R2-P1-ctl-02): an Erase whose
// context ends while a host hook holds a node's report queue erases
// nothing and says so; once the hook returned, Erase waits for the node's
// stop and erases, and no row of the node comes back afterwards. Before,
// Erase deleted the rows right after the Close that returned early, and
// the queue's final flush wrote them again when the hook returned.
func TestEraseWaitsForTheNodesStop(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	var nodes *sqlstore.Store
	site, dev, c, n := sitePair(t, fabrictest.Topology{}, func(cfg *controller.Config) {
		nodes = sqlstore.New(cfg.DB)
		// The store is written by the queue's final flush, not by a timer
		// that might run while the hook is held.
		cfg.Node.FlushInterval = time.Hour
		cfg.HooksFor = func(string) client.Hooks {
			return client.Hooks{Changed: func() {
				if !armed.Load() {
					return
				}
				select {
				case entered <- struct{}{}:
					<-release
				default:
				}
			}}
		}
	})
	t.Cleanup(releaseOnce) // a failing test still lets the queue end

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	armed.Store(true)
	if err := dev.Act(func(n *node.Node) error {
		return n.Root().BasicInformation.SetSoftwareVersion(77, "77")
	}); err != nil {
		t.Fatal(err)
	}
	if err := site.Run(ctx, func(ctx context.Context) error {
		select {
		case <-entered:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}); err != nil {
		t.Fatalf("the report's hook never ran: %v", err)
	}

	// The hook holds the queue; an Erase whose context has ended cannot
	// wait for the node's stop.
	ended, end := context.WithCancel(t.Context())
	end()
	if err := c.Erase(ended); err == nil {
		t.Fatal("Erase with the node still stopping returned nil")
	}
	if ids, err := nodes.NodeIDs(); err != nil || len(ids) == 0 {
		t.Fatalf("stored nodes after the Erase that could not wait: %v (%v), want the paired one kept", ids, err)
	}

	// The hook returns; Erase now waits for the stop and erases.
	releaseOnce()
	if err := site.Run(ctx, c.Erase); err != nil {
		t.Fatalf("Erase after the hook returned: %v", err)
	}
	if ids, err := nodes.NodeIDs(); err != nil || len(ids) != 0 {
		t.Fatalf("stored nodes right after the Erase: %v (%v), want none", ids, err)
	}
	// Whatever the node's queue had left has run: a second Close of the
	// node waits for its stop.
	if err := site.Run(ctx, n.Close); err != nil {
		t.Fatal(err)
	}
	if ids, err := nodes.NodeIDs(); err != nil || len(ids) != 0 {
		t.Fatalf("erased node rows came back: %v (%v)", ids, err)
	}
}

// TestConcurrentErasesOfAStoppingNode (review II, rework of R2-P1-ctl-02):
// two Erase calls that run at once after a Close left a node stopping run
// one after the other — the registry Close left is read and cleared under
// a lock — and both return nil; the node's rows are gone. Before, both
// read and cleared it unguarded (a data race under -race) and their store
// erases ran at once (one scrub blocked by the other's reader).
func TestConcurrentErasesOfAStoppingNode(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	var nodes *sqlstore.Store
	site, dev, c, _ := sitePair(t, fabrictest.Topology{}, func(cfg *controller.Config) {
		nodes = sqlstore.New(cfg.DB)
		cfg.Node.FlushInterval = time.Hour
		cfg.HooksFor = func(string) client.Hooks {
			return client.Hooks{Changed: func() {
				if !armed.Load() {
					return
				}
				select {
				case entered <- struct{}{}:
					<-release
				default:
				}
			}}
		}
	})
	t.Cleanup(releaseOnce)

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	armed.Store(true)
	if err := dev.Act(func(n *node.Node) error {
		return n.Root().BasicInformation.SetSoftwareVersion(77, "77")
	}); err != nil {
		t.Fatal(err)
	}
	if err := site.Run(ctx, func(ctx context.Context) error {
		select {
		case <-entered:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}); err != nil {
		t.Fatalf("the report's hook never ran: %v", err)
	}
	// A Close that cannot wait leaves the node stopping.
	ended, end := context.WithCancel(t.Context())
	end()
	if err := c.Close(ended); err == nil {
		t.Fatal("Close with the node still stopping returned nil")
	}
	releaseOnce()
	if err := site.Run(ctx, func(ctx context.Context) error {
		errs := make(chan error, 2)
		for range 2 {
			go func() { errs <- c.Erase(ctx) }()
		}
		for range 2 {
			if err := <-errs; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("concurrent Erase: %v", err)
	}
	if ids, err := nodes.NodeIDs(); err != nil || len(ids) != 0 {
		t.Fatalf("stored nodes after the Erases: %v (%v), want none", ids, err)
	}
}
