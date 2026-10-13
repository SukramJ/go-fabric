// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import "github.com/SukramJ/go-fabric/controller/interaction"

// ConnectionStateFor exposes the connection-state decision to the tests.
var ConnectionStateFor = connectionStateFor

// ObserveRequests sets the test seam that sees every subscription request
// the node hands to its interaction — the first one and each refreshed one.
func ObserveRequests(n *Node, f func(interaction.SubscribeRequest)) {
	n.mu.Lock()
	n.requestObserver = f
	n.mu.Unlock()
}
