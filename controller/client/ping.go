// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/clock"
)

var (
	// ErrNoSession: the node holds no operational session to run the
	// exchange on — the client machinery's [interaction.ErrNoSession].
	ErrNoSession = interaction.ErrNoSession
	// ErrUnreachable: the address did not answer [Node.PingAddress].
	ErrUnreachable = errors.New("client: address unreachable")
)

// pingMaxRetransmissions caps a ping's MRP transmissions — matter.js
// PROBE_MAX_RETRANSMISSIONS, "A liveness probe does not need the full MRP
// retransmission budget" (packages/protocol/src/peer/PeerAddressMonitor.ts:26-27
// at 862ad75f), the cap of the probe's only caller there.
const pingMaxRetransmissions = 2

// PingAddress probes one operational address of the node over the session
// it holds and returns the round trip, measured on the node's clock. It is
// matter.js ClientInteraction.probe with addressOverride
// (packages/protocol/src/action/client/ClientInteraction.ts:352-412 at
// 862ad75f), called as PeerAddressMonitor probes an alternate address
// (packages/protocol/src/peer/PeerAddressMonitor.ts:202-211, :229-237): an
// empty read on the existing session sent to addr, on the peer network's
// probe profile, with suppressed peer loss and at most
// PROBE_MAX_RETRANSMISSIONS transmissions. Unlike the monitor it never
// migrates: the session keeps its address, the record its addresses, and a
// failed ping does not count as a lost peer.
//
// Its purpose is per-address reachability in a host — go-fabric-ui's ping
// measures each address of [Record].Addresses with it. An address that
// does not answer is [ErrUnreachable], naming addr; a node not yet
// commissioned is [ErrNotCommissioned], one without a session
// [ErrNoSession], a closed one [ErrClosed]; the end of ctx is ctx's error.
// A ping waits for no ICD to wake: it is matter.js's protocol-level probe,
// not a ClientNodeInteraction call, and a sleeping LIT ICD fails it.
func (n *Node) PingAddress(ctx context.Context, addr netip.AddrPort) (time.Duration, error) {
	if n.isClosing() {
		return 0, ErrClosed
	}
	ia, err := n.interaction()
	if err != nil {
		return 0, err
	}
	if ia.Session() == nil {
		return 0, ErrNoSession
	}
	opts := interaction.ProbeOptions{
		AddressOverride: net.UDPAddrFromAddrPort(addr), SuppressPeerLoss: true, MaxRetransmissions: pingMaxRetransmissions,
	}
	// The peer's network, its probe profile when it has one
	// (PeerAddressMonitor.ts:202-203).
	if p := n.peer(); p != nil {
		network := p.Networks().ForPeer(p)
		if network.ProbeAddress != nil {
			network = network.ProbeAddress
		}
		opts.Network = network.ID
	}
	start := clock.MonotonicOf(n.clock)
	if !ia.Probe(ctx, opts) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("%w: %s", ErrUnreachable, addr)
	}
	return clock.MonotonicOf(n.clock) - start, nil
}
