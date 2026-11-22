// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/client/peer"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/im"
)

// NOCUpdateSeam is what the operational certificate update reaches the
// peer through (nocUpdateSeam), for the external tests' fault injection —
// matter.js's replaced ClientInteraction.prototype.invoke and read,
// Peer.prototype.connect, SessionManager.prototype.deleteResumptionRecord
// and ClientSubscriptions.prototype.closeForPeer.
type NOCUpdateSeam struct {
	Invoke                 func(ctx context.Context, ia *interaction.Interaction, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) (interaction.Result, error)
	Read                   func(ctx context.Context, ia *interaction.Interaction, paths []im.ConcreteAttributePath) ([]spec.Node, []bool, error)
	Connect                func(ctx context.Context, p *peer.Peer, opts peer.ConnectOptions) (*exchange.Session, error)
	DeleteResumptionRecord func(a handshake.PeerAddress) error
	CloseForPeer           func(a handshake.PeerAddress)
	Disconnect             func(ctx context.Context, p *peer.Peer, cause error)
	Opened                 func(ia *interaction.Interaction)
}

// PatchNOCUpdate replaces c's seam with what patch builds from the one in
// place; patches stack. restore puts the previous seam back.
func PatchNOCUpdate(c *Commissioner, patch func(current NOCUpdateSeam) NOCUpdateSeam) (restore func()) {
	prev := c.nocSeamOverride.Load()
	cur := c.nocSeam()
	next := patch(NOCUpdateSeam{
		Invoke: cur.invoke, Read: cur.read, Connect: cur.connect,
		DeleteResumptionRecord: cur.deleteResumptionRecord, CloseForPeer: cur.closeForPeer, Disconnect: cur.disconnect, Opened: cur.opened,
	})
	c.nocSeamOverride.Store(&nocUpdateSeam{
		invoke: next.Invoke, read: next.Read, connect: next.Connect,
		deleteResumptionRecord: next.DeleteResumptionRecord, closeForPeer: next.CloseForPeer, disconnect: next.Disconnect, opened: next.Opened,
	})
	return func() { c.nocSeamOverride.Store(prev) }
}

// SetForgetDisconnectBound sets the bound of the update's disconnect
// until restore.
func SetForgetDisconnectBound(d time.Duration) (restore func()) {
	prev := forgetDisconnectBound
	forgetDisconnectBound = d
	return func() { forgetDisconnectBound = prev }
}
