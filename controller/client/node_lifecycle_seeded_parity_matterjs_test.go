// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/NodeLifecycleSeededTest.ts at
// 862ad75f, the cases on a commissioned pair: a node with application
// endpoints seeds without the root-only warning, and a commissioned peer
// restored from storage is seeded before it connects. The report-fed cases
// are in node_lifecycle_seeded_parity_matterjs_internal_test.go.
package client_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// warnLog is a slog handler keeping the messages of WARN records.
type warnLog struct {
	mu   sync.Mutex
	msgs []string
}

func (h *warnLog) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }
func (h *warnLog) WithAttrs([]slog.Attr) slog.Handler           { return h }
func (h *warnLog) WithGroup(string) slog.Handler                { return h }
func (h *warnLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, r.Message)
	h.mu.Unlock()
	return nil
}

func (h *warnLog) count(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, m := range h.msgs {
		if m == msg {
			n++
		}
	}
	return n
}

func TestNodeLifecycleSeededPairParity(t *testing.T) {
	t.Parallel()

	t.Run("seeds a node with application endpoints without the warning", func(t *testing.T) {
		t.Parallel()
		logs := &warnLog{}
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
			rc.Node.Logger = slog.New(logs)
		}}})
		if len(cp.node.Endpoints()) <= 1 || !cp.node.Seeded() {
			t.Fatalf("endpoints %d, seeded %v", len(cp.node.Endpoints()), cp.node.Seeded())
		}
		if n := logs.count("matter.controller.client.root_only"); n != 0 {
			t.Fatalf("%d root-only warnings", n)
		}
	})

	t.Run("seeds a peer restored from storage while it is offline", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if !cp.node.Seeded() {
			t.Fatal("not seeded")
		}
		id := cp.node.ID()
		if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		n := cp.pair.Controller.Registry().Get(id)
		if n == nil || len(n.Endpoints()) <= 1 || !n.Seeded() || n.SubscriptionActive() {
			t.Fatalf("restored node %v: seeded without a connection", n)
		}
	})
}
