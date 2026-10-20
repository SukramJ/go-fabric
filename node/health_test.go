// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/node"
)

// sampleLog collects the probe's verdicts.
type sampleLog struct {
	mu      sync.Mutex
	samples []node.HealthSample
}

func (l *sampleLog) add(s node.HealthSample) {
	l.mu.Lock()
	l.samples = append(l.samples, s)
	l.mu.Unlock()
}

func (l *sampleLog) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.samples)
}

// waitSamples waits (wall clock) until log holds n samples: the probe
// records on its own goroutine.
func waitSamples(t *testing.T, log *sampleLog, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for log.len() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d samples, want %d", log.len(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// The health probe samples the listener at Start and on every interval
// of the node's clock, and Health returns the last verdict.
func TestHealthProbeSamplesOnTheNodesClock(t *testing.T) {
	ctx := context.Background()
	fake := clocktest.New(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
	log := &sampleLog{}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "n.db")))
	cfg.Clock = fake
	cfg.Health = node.Health{Interval: 10 * time.Second, OnSample: log.add}
	n := newNode(t, cfg)
	if _, ok := n.Health(); ok {
		t.Fatal("a sample before Start")
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitSamples(t, log, 1)
	s, ok := n.Health()
	if !ok || !s.Healthy || !strings.Contains(s.Note, n.LocalAddr()) {
		t.Fatalf("Health() = %+v %v, want healthy on %s", s, ok, n.LocalAddr())
	}
	fake.Advance(10 * time.Second)
	waitSamples(t, log, 2)
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	count := log.len()
	fake.Advance(time.Minute)
	if got := log.len(); got != count {
		t.Fatalf("%d samples after Stop, want %d", got, count)
	}
	if s, ok := n.Health(); !ok || !s.Healthy {
		t.Fatalf("Health() after Stop = %+v %v, want the last healthy verdict", s, ok)
	}
}

// The zero Health runs no probe; a negative interval is refused.
func TestHealthProbeOff(t *testing.T) {
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "n.db"))))
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.Health(); ok {
		t.Fatal("a sample without a probe")
	}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "m.db")))
	cfg.Health.Interval = -time.Second
	if err := cfg.Validate(); err == nil {
		t.Fatal("a negative Health.Interval validated")
	}
}
