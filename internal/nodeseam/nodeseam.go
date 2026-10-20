// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package nodeseam carries the one-way seam through which fabrictest puts
// a node's client stack onto its in-memory network.
//
// Direction of the edge: package node installs [UseClientListener] from
// its init; fabrictest reads it. node.Client names a bind address and a
// DNS-SD cache, which a host supplies; the socket itself is the node's own
// (udp.New), a kernel socket a site cannot carry. The seam hands the
// client stack a transport/udp.Listener over a socket of the site instead,
// as bridgeseam.UseListener does for the server socket, without a second
// exported way to build the stack.
package nodeseam

import "github.com/SukramJ/go-fabric/transport/udp"

// UseClientListener makes the node built from cfg (a *node.Config, as any:
// node imports this package to install the hook) open its client socket
// with open instead of udp.New. open is called once per client stack, at
// node.New; the client stack closes what it returns on Stop. Reports false
// when cfg is not a *node.Config or open is nil.
var UseClientListener func(cfg any, open func() (*udp.Listener, error)) bool
