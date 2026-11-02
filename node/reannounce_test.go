// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// reannouncer is an in-memory advertiser with an on-demand
// TriggerReannounce that counts the node's calls.
type reannouncer struct {
	*mdns.Noop
	mu       sync.Mutex
	triggers int
}

func (r *reannouncer) Enabled() bool { return true }

func (r *reannouncer) TriggerReannounce(context.Context) {
	r.mu.Lock()
	r.triggers++
	r.mu.Unlock()
}

// TestTriggerReannounce: Node.TriggerReannounce forwards to the
// advertiser's TriggerReannounce (issue #119), Start and Stop start and
// stop nothing for it — the broadcast schedule is the advertiser's
// (issue #362) — and on a Noop advertiser the trigger is a no-op.
func TestTriggerReannounce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adv := &reannouncer{Noop: mdns.NewNoop()}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	n := newNode(t, cfg)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	n.TriggerReannounce(ctx)
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	adv.mu.Lock()
	triggers := adv.triggers
	adv.mu.Unlock()
	if triggers != 1 {
		t.Errorf("TriggerReannounce forwarded %d times, want 1", triggers)
	}

	n = newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "noop.db"))))
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	n.TriggerReannounce(ctx)
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	var _ node.Advertiser = adv
}
