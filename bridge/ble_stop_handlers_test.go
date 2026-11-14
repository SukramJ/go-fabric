// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
)

// blockingOnOffServer is an OnOff server on the root endpoint whose
// MatterInvoke signals entered and then runs block.
type blockingOnOffServer struct {
	entered  chan struct{}
	block    func(ctx context.Context)
	finished atomic.Bool
}

func (*blockingOnOffServer) MatterClusterID() uint32                        { return 0x0006 }
func (*blockingOnOffServer) MatterRead(uint32) (any, bool)                  { return nil, false }
func (*blockingOnOffServer) MatterWrite(context.Context, uint32, any) error { return nil }
func (*blockingOnOffServer) MatterReportable() []uint32                     { return nil }

func (s *blockingOnOffServer) MatterInvoke(ctx context.Context, _ uint32, _ any) (any, error) {
	close(s.entered)
	s.block(ctx)
	s.finished.Store(true)
	return nil, nil
}

// startBLEInvoke attaches srv on the root endpoint, connects a central to
// the harness radio and sends an invoke that reaches srv; it returns once
// srv's handler runs.
func startBLEInvoke(t *testing.T, srv *blockingOnOffServer) *tcpHarness {
	t.Helper()
	h, p := newBLEHarness(t, nil)
	h.bridge.AttachRootClusters([]contract.ClusterServer{srv})
	if err := h.bridge.Reassemble(t.Context()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	c := dialRadio(t, p)
	if err := c.s.Send(h.sealedInvoke(0x330)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-srv.entered:
	case <-ctx.Done():
		t.Fatal("the invoke over BLE never reached the server")
	}
	return h
}

// TestStopWaitsForBLEHandlers (review II, R2-P1-req-05): Stop does not
// return while the handler of a message delivered over BLE still runs —
// the node closes or erases its store after Stop, and a handler running on
// would write into it — unless Stop's own context ends first.
func TestStopWaitsForBLEHandlers(t *testing.T) {
	release := make(chan struct{})
	srv := &blockingOnOffServer{
		entered: make(chan struct{}),
		block:   func(context.Context) { <-release },
	}
	h := startBLEInvoke(t, srv)
	defer close(release)

	stopCtx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_ = h.bridge.Stop(stopCtx)
	if !srv.finished.Load() && stopCtx.Err() == nil {
		t.Fatal("Stop returned while a handler of a BLE message still ran, before its context ended")
	}
}

// TestStopReturnsOnceBLEHandlersEnd: a BLE handler that honours the
// cancelled handler context ends, and Stop returns after it, without
// waiting for its own deadline.
func TestStopReturnsOnceBLEHandlersEnd(t *testing.T) {
	srv := &blockingOnOffServer{
		entered: make(chan struct{}),
		block:   func(ctx context.Context) { <-ctx.Done() },
	}
	h := startBLEInvoke(t, srv)

	stopCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_ = h.bridge.Stop(stopCtx)
	if stopCtx.Err() != nil {
		t.Fatalf("Stop ran into its deadline: %v", stopCtx.Err())
	}
	if !srv.finished.Load() {
		t.Fatal("Stop returned before the BLE handler ended")
	}
}
