// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/contract"
)

var _ contract.ClusterQuiescer = (*camera.PushTransport)(nil)

// TestPushTransportQuiesceStopsTheExpiry holds the contract of
// contract.ClusterQuiescer for the PushTransport: after MatterQuiesce no
// expiry timer is armed, and the connection's expiry neither reaches the
// port nor changes CurrentConnections. A reassembly that puts the same
// server back (SetEndpoint) arms the expiry again for the time left to its
// deadline, as EvseServer re-arms its own; one that passed while the
// server was out fires at once. Before, the package had no
// MatterQuiesce and the timer deallocated through the port after the
// server was gone (review II, R2-P6-conc-1606, N-clumisc-4).
func TestPushTransportQuiesceStopsTheExpiry(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_000_000, 0))
	p, m := newPush(t, func(c *camera.PushTransportConfig) { c.Clock = clk })
	o := motionOptions()
	expiry := uint32(30)
	o.ExpiryTime = &expiry
	allocatePush(t, p, o)
	clk.Advance(10 * time.Second)

	p.MatterQuiesce()
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers armed after MatterQuiesce", n)
	}
	clk.Advance(5 * time.Second)
	if len(m.deallocated) != 0 || len(p.Connections()) != 1 {
		t.Fatalf("after MatterQuiesce: deallocated %v, connections %d", m.deallocated, len(p.Connections()))
	}

	// Put back: the deadline stands, 15 s are left.
	p.SetEndpoint(1)
	clk.Advance(14 * time.Second)
	if len(m.deallocated) != 0 {
		t.Fatal("expired before the time left")
	}
	clk.Advance(time.Second)
	if len(m.deallocated) != 1 || len(p.Connections()) != 0 {
		t.Fatalf("after the remount: deallocated %v, connections %d", m.deallocated, len(p.Connections()))
	}
}

// TestPushTransportRemountAfterTheDeadlineExpiresAtOnce: an expiry whose
// deadline passed while the server was quiesced deallocates at the remount.
func TestPushTransportRemountAfterTheDeadlineExpiresAtOnce(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_000_000, 0))
	p, m := newPush(t, func(c *camera.PushTransportConfig) { c.Clock = clk })
	o := motionOptions()
	expiry := uint32(30)
	o.ExpiryTime = &expiry
	allocatePush(t, p, o)
	p.MatterQuiesce()
	clk.Advance(time.Minute)
	if len(m.deallocated) != 0 {
		t.Fatal("expired while quiesced")
	}
	p.SetEndpoint(1)
	clk.Advance(0)
	if len(m.deallocated) != 1 || len(p.Connections()) != 0 {
		t.Fatalf("after the remount: deallocated %v, connections %d", m.deallocated, len(p.Connections()))
	}
}

// TestPushAllocateAfterQuiesceArmsNoTimer (R2-P6-conc-1606 follow-on): an
// allocation in flight across MatterQuiesce — here one that commits after
// it — keeps its expiry deadline but arms no timer; SetEndpoint arms it
// for the time left. Before, commitAllocation armed the expiry on the
// quiesced server.
func TestPushAllocateAfterQuiesceArmsNoTimer(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_000_000, 0))
	p, m := newPush(t, func(c *camera.PushTransportConfig) { c.Clock = clk })
	p.MatterQuiesce()
	o := motionOptions()
	expiry := uint32(30)
	o.ExpiryTime = &expiry
	allocatePush(t, p, o)
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers armed on the quiesced server", n)
	}
	clk.Advance(10 * time.Second)
	p.SetEndpoint(1)
	clk.Advance(19 * time.Second)
	if len(m.deallocated) != 0 {
		t.Fatal("expired before the time left")
	}
	clk.Advance(time.Second)
	if len(m.deallocated) != 1 || len(p.Connections()) != 0 {
		t.Fatalf("after the remount: deallocated %v, connections %d", m.deallocated, len(p.Connections()))
	}
}
