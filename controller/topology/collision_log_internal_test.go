// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	threaddiagdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
)

// records counts the messages logged.
type records struct {
	mu   sync.Mutex
	msgs map[string]int
}

func (r *records) Enabled(context.Context, slog.Level) bool { return true }
func (r *records) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.msgs == nil {
		r.msgs = map[string]int{}
	}
	r.msgs[rec.Message]++
	return nil
}
func (r *records) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *records) WithGroup(string) slog.Handler      { return r }

func (r *records) count(msg string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.msgs[msg]
}

// TestServiceLogsAClaimCollisionOnce (review II, R2-P6-bound-2362, owner
// decision 222): with Config.Logger, the service logs a claim two nodes
// make once, however often it builds; without one it logs nothing.
func TestServiceLogsAClaimCollisionOnce(t *testing.T) {
	a := mkThread(1, threadOpts{role: role(threaddiagdef.RoutingRoleRouter), rloc16: ptr[uint16](0x0400), ext: 0x0A0B0C0D0E0F1011})
	b := mkThread(2, threadOpts{role: role(threaddiagdef.RoutingRoleRouter), rloc16: ptr[uint16](0x0800), ext: 0x0A0B0C0D0E0F1011})
	rec := &records{}
	h := newHarness(t, Config{Logger: slog.New(rec)}, a, b)
	for range 3 {
		_ = h.svc.Topology()
	}
	if n := rec.count("matter.controller.topology.claim_collision"); n != 1 {
		t.Fatalf("the collision was logged %d times, want once", n)
	}
	quiet := newHarness(t, Config{}, a, b)
	_ = quiet.svc.Topology() // no logger: nothing to log to, nothing panics
}
