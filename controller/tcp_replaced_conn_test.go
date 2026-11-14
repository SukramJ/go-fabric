// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/operational"
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

	readLarge()
	again := p.NewestSessionOn(exchange.TransportTCP)
	if again == nil || again == first {
		t.Fatalf("after its connection was replaced, the controller read over TCP without a new session (%v)", again)
	}
}
