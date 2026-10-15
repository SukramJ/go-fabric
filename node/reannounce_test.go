// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// reannouncer is an in-memory advertiser that implements mdns.Reannouncer
// and mdns.Enabled and records how the node drives them.
type reannouncer struct {
	*mdns.Noop
	mu        sync.Mutex
	intervals []time.Duration
	cancels   int
	triggers  int
}

func (r *reannouncer) Enabled() bool { return true }

func (r *reannouncer) StartReannounceLoop(_ context.Context, interval time.Duration) (cancel func(), trigger chan<- struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.intervals = append(r.intervals, interval)
	return func() {
		r.mu.Lock()
		r.cancels++
		r.mu.Unlock()
	}, make(chan struct{}, 1)
}

func (r *reannouncer) TriggerReannounce(context.Context) {
	r.mu.Lock()
	r.triggers++
	r.mu.Unlock()
}

func (r *reannouncer) state() (intervals []time.Duration, cancels, triggers int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.intervals...), r.cancels, r.triggers
}

// TestReannounceLoop (issue #119): Start starts the advertiser's loop at
// the configured interval (DefaultReannounceInterval for 0), Stop ends it
// once, TriggerReannounce is forwarded; DisableReannounce starts none; a
// Noop advertiser is not a Reannouncer and the trigger is a no-op on it.
func TestReannounceLoop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		interval time.Duration
		disable  bool
		want     []time.Duration
	}{
		{"configured", 7 * time.Minute, false, []time.Duration{7 * time.Minute}},
		{"default", 0, false, []time.Duration{node.DefaultReannounceInterval}},
		{"disabled", 7 * time.Minute, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adv := &reannouncer{Noop: mdns.NewNoop()}
			cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
			cfg.Advertiser = adv
			cfg.Network.ReannounceInterval = tc.interval
			cfg.Network.DisableReannounce = tc.disable
			n := newNode(t, cfg)
			if got, _, _ := adv.state(); len(got) != 0 {
				t.Fatalf("loop started by New: %v", got)
			}
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			got, cancels, _ := adv.state()
			if len(got) != len(tc.want) || (len(got) == 1 && got[0] != tc.want[0]) {
				t.Fatalf("loop intervals %v, want %v", got, tc.want)
			}
			if cancels != 0 {
				t.Fatalf("loop cancelled %d times before Stop", cancels)
			}
			n.TriggerReannounce(ctx)
			if _, _, triggers := adv.state(); triggers != 1 {
				t.Errorf("TriggerReannounce forwarded %d times, want 1", triggers)
			}
			if !mdns.IsEnabled(cfg.Advertiser) {
				t.Error("the fake advertiser reads as disabled")
			}
			if err := n.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if err := n.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			wantCancels := len(tc.want)
			if _, cancels, _ := adv.state(); cancels != wantCancels {
				t.Errorf("loop cancelled %d times by Stop, want %d", cancels, wantCancels)
			}
		})
	}

	// A Noop advertiser runs no loop; the trigger is a no-op.
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "noop.db"))))
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	n.TriggerReannounce(ctx)
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "bad.db")))
	cfg.Network.ReannounceInterval = -time.Second
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Errorf("negative interval: %v", err)
	}
}
