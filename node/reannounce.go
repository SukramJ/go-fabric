// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"strings"

	"github.com/SukramJ/go-fabric/mdns"
)

// TriggerReannounce announces the node's DNS-SD records now — for a host
// that observes a link change (interface up, address assigned) and should
// not wait for the advertiser's broadcast schedule. It forwards to the
// advertiser's TriggerReannounce (mdns.Responder.TriggerReannounce: one
// publication of every published service per interface, with the goodbyes
// of the addresses that left, matter.js MdnsServer.broadcast) and is a
// no-op on an advertiser without one (mdns.Noop, a custom Advertiser). The
// responder also follows the interfaces itself at
// [Network].InterfaceCheck.
func (n *Node) TriggerReannounce(ctx context.Context) {
	if r, ok := n.advertiser.(interface{ TriggerReannounce(context.Context) }); ok {
		r.TriggerReannounce(ctx)
	}
}

// operationalConnected ends the operational broadcast of the fabric a CASE
// session opened on: matter.js's DeviceAdvertiser notifies the fabric's
// advertisement of the new session (DeviceAdvertiser.ts:114-129 at
// 862ad75f), whose DefaultBroadcastSchedule stops at the first connection
// (broadcastAfterConnection Instant, MdnsAdvertiser.ts:230-231). The
// service keeps answering queries. A no-op on an advertiser without
// ServiceConnected (mdns.Noop, a custom Advertiser).
func (n *Node) operationalConnected(fabricIndex uint8) {
	a, ok := n.advertiser.(interface {
		ServiceConnected(instanceName, serviceType string)
	})
	if !ok || n.caseIDs == nil || fabricIndex == 0 {
		return
	}
	compressedID, nodeID, ok := n.caseIDs.announceIdentity(fabricIndex)
	if !ok {
		return
	}
	instance, _, _ := strings.Cut(mdns.OperationalInstanceQName(compressedID, nodeID), ".")
	a.ServiceConnected(instance, mdns.ServiceTypeOperational)
}
