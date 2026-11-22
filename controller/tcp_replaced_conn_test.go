// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	basicinfo "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/transport/tcp"
)

// TestControllerConnectsAgainAfterItsIdleTCPConnectionWasReplaced: a
// device at its bound of inbound TCP connections gives the place of a
// connection idle past the secure session's idle timeout to a new one
// (review II, R2-P3-dr-net-02). The controller whose connection it was
// loses the session on it and, for its next interaction over TCP, opens a
// new connection and a new session.
func TestControllerConnectsAgainAfterItsIdleTCPConnectionWasReplaced(t *testing.T) {
	t.Parallel()
	tp := newTCPPairWith(t, fabrictest.ControllerOptions{TCP: controller.TCPConfig{Outgoing: true}},
		&node.TCPConfig{Incoming: true, Outgoing: true, MaxInboundConnections: 1})
	large := true
	readLarge := func() {
		t.Helper()
		tp.run(t, func(ctx context.Context) error {
			var req client.ReadRequest
			req.Attributes = []client.AttributePath{client.AttributeAt(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)}
			req.LargeMessage = &large
			_, err := tp.n.Read(ctx, req)
			return err
		})
	}
	readLarge()
	p := tp.peer(t)
	first := p.NewestSessionOn(exchange.TransportTCP)
	if first == nil {
		t.Fatal("no TCP session after a large-message read")
	}

	// The site's clock steps past the grace without firing a timer, so no
	// session is reaped for idleness: what closes the connection is the
	// device's listener, which a further connection reaches at its bound.
	fake := tp.site.Fake()
	fake.Set(fake.Now().Add(operational.SessionIdleTimeout + time.Second))
	a := tp.dev.Addr()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	var d net.Dialer
	other, err := d.DialContext(ctx, "tcp", net.JoinHostPort(a.IP.String(), strconv.Itoa(a.Port)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	select {
	case <-first.Closing():
	case <-ctx.Done():
		t.Fatal("the controller's TCP session stayed open after the device gave its idle connection's place to a new one")
	}
	// The silent connection now holds the one place; past the age that
	// spares a fresh connection (a fraction of tcp.NewConnectionIdleTimeout,
	// review II R2-P6-bound-0203) the controller's next connection takes it.
	fake.Set(fake.Now().Add(tcp.NewConnectionIdleTimeout / 2))

	readLarge()
	again := p.NewestSessionOn(exchange.TransportTCP)
	if again == nil || again == first {
		t.Fatalf("after its connection was replaced, the controller read over TCP without a new session (%v)", again)
	}
}
