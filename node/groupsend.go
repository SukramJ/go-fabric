// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"log/slog"

	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// *udp.Listener is the transport group messages leave through.
var _ groups.MulticastTransport = (*udp.Listener)(nil)

// groupSendAddr is the bind address of the socket group messages are sent
// from: any IPv6 address, an ephemeral port. Group messages are IPv6
// multicast and carry no answer, so the source port is nobody's concern.
const groupSendAddr = "[::]:0"

// startGroupSender opens the socket the node's group messages leave
// through and hands it to the group state, so [Node.Groups]'s
// [groups.Manager.Sender] can send — the UDP interface matter.js
// GroupSession.create opens its channel on (packages/protocol/src/session/
// GroupSession.ts:143-153 at 0ceb202d). matter.js sends from its
// operational socket; the bridge keeps that socket to itself, so the node
// sends from one of its own. Without IPv6 no socket opens, and Sender
// answers groups.ErrNoInterface. Called by Start with n.mu held.
func (n *Node) startGroupSender() {
	l, err := udp.New(udp.Config{LocalAddr: groupSendAddr})
	if err != nil {
		n.logger.Warn("matter.groups.send_socket", slog.String("err", err.Error()))
		return
	}
	n.groupTx = l
	n.groups.SetTransport(l)
}

// stopGroupSender detaches and closes the group send socket. Called by
// Stop with n.mu held.
func (n *Node) stopGroupSender() {
	if n.groupTx == nil {
		return
	}
	n.groups.SetTransport(nil)
	_ = n.groupTx.Close()
	n.groupTx = nil
}
