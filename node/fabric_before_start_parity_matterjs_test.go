// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/commissioning/FabricBeforeStartTest.ts
// at 862ad75f: a node whose store holds a fabric when it starts counts as
// commissioned and advertises the fabric operationally once, after it
// stopped counting as commissionable, and logs no error doing so.
//
// matter.js builds a controller node, creates its fabric
// (FabricAuthority.defaultFabric) before start and records
// CommissioningServer.enterOperationalMode. A Go node does not create a
// fabric of its own; the fabric a node finds in its store at start is one a
// commissioner installed before a restart. So the port commissions a
// fabrictest device, stops it and starts it again on the store, and records
// the operational records its advertiser publishes from that start
// (enterOperationalMode announces them). Commissioned is the node holding
// the fabric with no window open and no commissionable record; the
// lifecycle.commissioned event matter.js counts has no Go counterpart at
// start — Observers.OnCommissioningComplete fires per CommissioningComplete
// (BD-Matter-Node-CommissioningCompletePerCommand). fabricsChanged is the
// node's fabric observers (Observers.OnFabricAdded / OnFabricRemoved /
// OnFabricUpdated). "logs no error when the fabric write settles while the
// node goes online" holds the fabric write back with a gated storage driver
// until the node goes online; the Go store writes the fabric before the
// command answers and the node reads it before it starts, so the port is
// the start on the written store with its error log, fabric events and
// state.
//
// Missing (register): "waits for another transaction that holds the
// commissioning state" — js-runtime-only: the commissioning-state
// transaction lock (as the register's FabricChangeLockTest.ts).
package node_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// operationalSpy is an advertiser that records each operational record it
// publishes and whether a commissionable record was active at that moment.
type operationalSpy struct {
	*mdns.Noop
	mu sync.Mutex
	// operational counts the operational publishes; commissionableAt counts
	// those made while a commissionable record was published.
	operational, commissionableAt int
}

func (s *operationalSpy) Publish(ctx context.Context, svc mdns.Service) error {
	if svc.ServiceType == mdns.ServiceTypeOperational {
		commissionable := false
		active := s.Active()
		for i := range active {
			if active[i].ServiceType == mdns.ServiceTypeCommissionable {
				commissionable = true
			}
		}
		s.mu.Lock()
		s.operational++
		if commissionable {
			s.commissionableAt++
		}
		s.mu.Unlock()
	}
	return s.Noop.Publish(ctx, svc)
}

func (s *operationalSpy) counts() (operational, commissionableAt int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.operational, s.commissionableAt
}

// errorLog records the records of level ERROR and above.
type errorLog struct {
	mu      sync.Mutex
	records []string
}

func (h *errorLog) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelError }

func (h *errorLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r.Message)
	h.mu.Unlock()
	return nil
}
func (h *errorLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *errorLog) WithGroup(string) slog.Handler      { return h }

func (h *errorLog) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.records...)
}

// fabricBeforeStart is a device started on a store that holds a fabric:
// commissioned, stopped, and started again with a fresh spy; the error log
// and the fabric events record that start.
type fabricBeforeStart struct {
	device  *fabrictest.Device
	spy     *operationalSpy
	errors  *errorLog
	mu      sync.Mutex
	changes []string
}

func startWithFabric(t *testing.T) *fabricBeforeStart {
	t.Helper()
	holdSite(t)
	f := &fabricBeforeStart{errors: &errorLog{}}
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(f.errors)})
	var recording bool
	record := func(change string) {
		f.mu.Lock()
		if recording {
			f.changes = append(f.changes, change)
		}
		f.mu.Unlock()
	}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
		spy := &operationalSpy{Noop: mdns.NewNoop()}
		f.mu.Lock()
		f.spy = spy
		f.mu.Unlock()
		cfg.Advertiser = spy
		cfg.Observers.OnFabricAdded = func(node.FabricInfo) error { record("added"); return nil }
		cfg.Observers.OnFabricRemoved = func(node.FabricInfo) error { record("deleted"); return nil }
		cfg.Observers.OnFabricUpdated = func(node.FabricInfo) error { record("updated"); return nil }
	}}})
	f.device = p.Device
	if err := f.device.Stop(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	f.errors.mu.Lock()
	f.errors.records = nil
	f.errors.mu.Unlock()
	f.mu.Lock()
	recording = true
	f.mu.Unlock()
	if err := f.device.Start(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	if err := site.Settle(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	return f
}

// current is the spy of the last start.
func (f *fabricBeforeStart) current() *operationalSpy {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spy
}

// commissioned is CommissioningServer state.commissioned and
// lifecycle.isCommissioned: a fabric, no window, no commissionable record.
func (f *fabricBeforeStart) commissioned(t *testing.T) bool {
	t.Helper()
	fabrics, err := f.device.Node().Fabrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	active := f.current().Active()
	for i := range active {
		if active[i].ServiceType == mdns.ServiceTypeCommissionable {
			return false
		}
	}
	return len(fabrics) == 1 && !f.device.Node().CommissioningWindow().IsOpen()
}

// TestFabricBeforeStart ports the describe "A fabric created before the
// node starts" (FabricBeforeStartTest.ts:59-171) but the transaction lock
// case.
func TestFabricBeforeStart(t *testing.T) {
	t.Run("counts the node as commissioned and advertises it operationally once online", func(t *testing.T) {
		f := startWithFabric(t)
		if !f.commissioned(t) {
			t.Error("the node does not count as commissioned")
		}
		if op, _ := f.current().counts(); op != 1 {
			t.Errorf("operational advertisements %d, want 1", op)
		}
	})

	t.Run("logs no error when the fabric write settles while the node goes online", func(t *testing.T) {
		f := startWithFabric(t)
		if msgs := f.errors.messages(); len(msgs) != 0 {
			t.Errorf("errors logged: %q", msgs)
		}
		f.mu.Lock()
		changes := append([]string(nil), f.changes...)
		f.mu.Unlock()
		if len(changes) != 0 {
			t.Errorf("fabricsChanged %v, want none", changes)
		}
		if !f.commissioned(t) {
			t.Error("the node does not count as commissioned")
		}
	})

	t.Run("commits the commissioned state before it advertises the node operationally", func(t *testing.T) {
		f := startWithFabric(t)
		op, commissionableAt := f.current().counts()
		if op != 1 {
			t.Errorf("operational advertisements %d, want 1", op)
		}
		if commissionableAt != 0 {
			t.Error("the node advertised operationally while it was still commissionable")
		}
	})
}
