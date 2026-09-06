// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
)

// newBridge starts a bridge on the loopback interface with an empty fleet.
// Everything here is public API plus endpointtest — the same construction a
// consumer outside this module writes.
func newBridge(t *testing.T) *bridge.Bridge {
	t.Helper()
	b, err := bridge.New(
		endpointtest.NewEmptySnapshotter(),
		mdns.NewNoop(),
		bridge.Config{
			Listen:     "127.0.0.1:0",
			PreferIPv4: true,
			VendorID:   0x1234,
			ProductID:  0x5678,
			NodeLabel:  "bridgetest",
		},
		nil,
	)
	if err != nil {
		t.Fatalf("bridge.New: %v", err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatalf("bridge.Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = b.Stop(ctx)
	})
	return b
}

// newPeer opens a loopback UDP socket standing in for the commissioner.
func newPeer(t *testing.T) (*net.UDPConn, *net.UDPAddr) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("peer ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer addr type %T", conn.LocalAddr())
	}
	return conn, addr
}

// newSubscription registers a subscription in a manager the bridge reports
// through, and returns it. This is the half of "establish a subscription"
// that the public API already covers.
func newSubscription(t *testing.T, b *bridge.Bridge) *subscription.Subscription {
	t.Helper()
	m := subscription.NewManager(subscription.Config{}, b.SubscriptionReporter(), nil)
	b.AttachSubscriptionManager(m)
	sub, err := m.Subscribe(subscription.SubscribeArgs{
		MinIntervalFloor:   1,
		MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{
			{Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005},
		},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return sub
}

// awaitDatagram reports whether a datagram arrives on conn within the
// deadline. The false case is the one the negative control below reads, so
// a read error other than a timeout is a test failure rather than a false.
func awaitDatagram(t *testing.T, conn *net.UDPConn, within time.Duration) bool {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(within)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 1500)
	_, _, err := conn.ReadFromUDP(buf)
	if err == nil {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	t.Fatalf("peer ReadFromUDP: %v", err)
	return false
}

// TestEstablishSubscriptionTarget_RoutesAReport is the effect the package
// exists for: after establishing the target, a report handed to the
// bridge's own reporter reaches the peer — with no SubscribeRequest ever
// sent, so nothing but the report itself is on the wire.
func TestEstablishSubscriptionTarget_RoutesAReport(t *testing.T) {
	t.Parallel()

	b := newBridge(t)
	conn, addr := newPeer(t)
	sub := newSubscription(t, b)

	if err := bridgetest.EstablishSubscriptionTarget(b, sub.ID, bridgetest.SubscriptionTarget{
		Peer:       addr,
		ExchangeID: 7,
	}); err != nil {
		t.Fatalf("EstablishSubscriptionTarget: %v", err)
	}

	b.SubscriptionReporter()(t.Context(), sub, sub.AttributePaths)

	if !awaitDatagram(t, conn, 2*time.Second) {
		t.Fatal("no report reached the peer after establishing its target")
	}
}

// TestReportIsNotRoutedWithoutATarget is the negative control for the test
// above: the identical sequence minus the establish call must leave the
// peer silent. Without it, a report that reached the peer for some other
// reason would read as a pass.
func TestReportIsNotRoutedWithoutATarget(t *testing.T) {
	t.Parallel()

	b := newBridge(t)
	conn, _ := newPeer(t)
	sub := newSubscription(t, b)

	b.SubscriptionReporter()(t.Context(), sub, sub.AttributePaths)

	if awaitDatagram(t, conn, 500*time.Millisecond) {
		t.Fatal("a report reached the peer although no target was established")
	}
}

// TestEstablishSubscriptionTarget_Rejections covers the three argument
// shapes that cannot describe a route.
func TestEstablishSubscriptionTarget_Rejections(t *testing.T) {
	t.Parallel()

	b := newBridge(t)
	_, addr := newPeer(t)

	tests := []struct {
		name   string
		bridge *bridge.Bridge
		subID  uint32
		target bridgetest.SubscriptionTarget
		want   error
	}{
		{"nil bridge", nil, 1, bridgetest.SubscriptionTarget{Peer: addr}, bridgetest.ErrNoBridge},
		{"subscription id 0", b, 0, bridgetest.SubscriptionTarget{Peer: addr}, bridgetest.ErrNoSubscriptionID},
		{"no peer", b, 1, bridgetest.SubscriptionTarget{}, bridgetest.ErrNoPeer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := bridgetest.EstablishSubscriptionTarget(tc.bridge, tc.subID, tc.target)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestAckPumpTick_EmitsNothingWithoutATracker pins the no-tracker case: the
// tick is safe to call on a bridge the daemon never wired an AckTracker
// into, and reports that it emitted nothing.
func TestAckPumpTick_EmitsNothingWithoutATracker(t *testing.T) {
	t.Parallel()

	b := newBridge(t)
	emitted, err := bridgetest.AckPumpTick(b, time.Now())
	if err != nil {
		t.Fatalf("AckPumpTick: %v", err)
	}
	if emitted != 0 {
		t.Errorf("emitted = %d, want 0 without an attached tracker", emitted)
	}
}

// TestAckPumpTick_NilBridge keeps the nil case an error rather than a panic
// inside the seam.
func TestAckPumpTick_NilBridge(t *testing.T) {
	t.Parallel()

	if _, err := bridgetest.AckPumpTick(nil, time.Now()); !errors.Is(err, bridgetest.ErrNoBridge) {
		t.Errorf("err = %v, want ErrNoBridge", err)
	}
}
