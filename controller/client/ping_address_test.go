// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/controller/client"
)

// ping runs n.PingAddress(addr) on the pair's site, its clock moved
// through the MRP retransmissions.
func (cp *clientPair) ping(t *testing.T, n *client.Node, addr netip.AddrPort) (time.Duration, error) {
	t.Helper()
	var (
		rtt  time.Duration
		perr error
	)
	if err := cp.site.Run(parityCtx(t), func(ctx context.Context) error {
		rtt, perr = n.PingAddress(ctx, addr)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return rtt, perr
}

// TestPingAddress is the acceptance of issue #398 on a commissioned pair:
// a ping of the device's address answers, a ping of an address on the
// in-memory network nobody listens on fails naming it — and neither moves
// the session's address, the record's addresses or the peer's liveness
// (matter.js ClientInteraction.probe with addressOverride,
// packages/protocol/src/action/client/ClientInteraction.ts:352-412 at
// 862ad75f).
func TestPingAddress(t *testing.T) {
	t.Parallel()
	cp := newClientPair(t, clientPairOptions{rootOnly: true})
	n := cp.node
	device := cp.pair.Device.Addr().AddrPort()
	// The device's host, a port nobody listens on.
	silent := netip.AddrPortFrom(device.Addr(), device.Port()+1)
	before := slices.Clone(n.Record().Addresses)

	if _, err := cp.ping(t, n, device); err != nil {
		t.Fatalf("ping of the device's address: %v", err)
	}
	_, err := cp.ping(t, n, silent)
	if !errors.Is(err, client.ErrUnreachable) || !containsAddr(err, silent) {
		t.Fatalf("ping of a silent address: %v, want ErrUnreachable naming %s", err, silent)
	}
	if !slices.Equal(n.Record().Addresses, before) {
		t.Fatalf("record addresses %v after the pings, want %v", n.Record().Addresses, before)
	}
	// The failed ping suppressed the peer loss: the node keeps its
	// session and its subscription, and the next ping on the device's
	// address answers.
	if !n.SubscriptionActive() {
		t.Fatal("the failed ping ended the default subscription")
	}
	if _, err := cp.ping(t, n, device); err != nil {
		t.Fatalf("ping of the device's address after a failed one: %v", err)
	}
	ctx, cancel := context.WithCancel(parityCtx(t))
	cancel()
	if _, err := n.PingAddress(ctx, device); !errors.Is(err, context.Canceled) {
		t.Fatalf("ping with an ended ctx: %v, want context.Canceled", err)
	}
}

// TestPingAddressRefusals: a node without a session and a closed node
// ping nothing.
func TestPingAddressRefusals(t *testing.T) {
	t.Parallel()
	addr := netip.MustParseAddrPort("[fd00::1]:5540")
	s := newScripted(t)
	bare, err := client.New(client.Config{ID: "peer1", Interaction: interaction.Bind(interaction.SessionProvider(s.mgr, nil), interaction.Config{})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.PingAddress(t.Context(), addr); !errors.Is(err, client.ErrNoSession) || !errors.Is(err, interaction.ErrNoSession) {
		t.Fatalf("ping without a session: %v, want ErrNoSession", err)
	}
	n := newScriptedNode(t, client.Config{ID: "peer2"}).node
	if err := n.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := n.PingAddress(t.Context(), addr); !errors.Is(err, client.ErrClosed) {
		t.Fatalf("ping of a closed node: %v, want ErrClosed", err)
	}
}

// TestDecommissionProbeParity ports DecommissionTest "probe resolves false
// on a destroyed node instead of throwing"
// (packages/node/test/node/DecommissionTest.ts:301-320 at 862ad75f): the
// decommission completes with RemoveFabric's answer missing, and a late
// ping of the removed node fails instead of panicking — matter.js's false
// is the Go error.
func TestDecommissionProbeParity(t *testing.T) {
	t.Parallel()
	cp := newClientPair(t, clientPairOptions{})
	device := cp.pair.Device.Addr().AddrPort()
	restore := client.PatchRemoveFabric(cp.pair.Controller.Registry(), func(uint8, func(uint8) (client.Result, error)) (client.Result, error) {
		return client.Result{}, &exchange.PeerMessageMissingError{Timeout: 11 * time.Second}
	})
	err := cp.decommission(t)
	restore()
	if err != nil {
		t.Fatalf("decommission: %v", err)
	}
	if _, err := cp.ping(t, cp.node, device); !errors.Is(err, client.ErrClosed) {
		t.Fatalf("ping of a decommissioned node: %v, want ErrClosed", err)
	}
}

func containsAddr(err error, addr netip.AddrPort) bool {
	return err != nil && strings.Contains(err.Error(), addr.String())
}
