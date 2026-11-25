// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/endpoint"
)

// WebRTCRequestorConfig is the node's part of the WebRtcTransportRequestor
// servers its topology mounts ([camera.Requestor], the default of
// device.NewCameraController): which fabrics the cameras reach them on.
type WebRTCRequestorConfig struct {
	// GrantFabric decides, per fabric of the node, whether the node writes
	// the requestor's ACL entry into that fabric's ACL: Operate for CASE,
	// no subject, the requestor's endpoint and cluster, one entry for each
	// endpoint that serves a [camera.Requestor] — every node of the fabric,
	// a camera among them, may then invoke the requestor's commands. nil
	// writes it into no fabric. matter.js writes it only into the first
	// fabric its node's own controller administers, by root certificate
	// (WebRtcTransportRequestorServer.ts:56-94 at db021da0,
	// fabricAuthority.fabrics[0] at :58; FabricAuthority.ts:112-114,
	// :126-128), and a [Node] has no controller of its own, so no fabric
	// gets it unasked: the fabric's administrator grants the access, or the
	// host returns true here for the fabrics its cameras signal on. The node
	// asks at Start and after each CommissioningComplete, on the calling
	// goroutine.
	//
	// A grant only ever adds the entry. Withdrawing it — false for a fabric
	// granted before, or GrantFabric left nil — removes nothing: the entry
	// a granting start wrote, like the one a version before GrantFabric
	// wrote into the first fabric's ACL, stays in that fabric's ACL, because
	// the node cannot tell an entry it wrote from one the fabric's
	// administrator wrote. The fabric's administrator removes it by writing
	// the fabric's ACL without it.
	GrantFabric func(fabric FabricInfo) bool
}

// ensureRequestorACL lets the camera that signals a WebRTC stream reach
// the controller's WebRtcTransportRequestor: an Operate entry for CASE, no
// subject, the requestor's endpoint and cluster — added for each endpoint
// that serves the kit's requestor ([camera.Requestor]) to each fabric
// [WebRTCRequestorConfig].GrantFabric grants whose ACL has none, and to no
// fabric without GrantFabric. matter.js adds it to the first fabric its
// own controller administers
// (packages/node/src/behaviors/web-rtc-transport-requestor/
// WebRtcTransportRequestorServer.ts:45-94 at db021da0: initialize,
// #nodeOnline, fabricAuthority.fabrics[0] at :58, the duplicate check
// :67-79, the entry :81-91; FabricAuthority.ts:112-114, :126-128: the
// fabrics whose root certificate is its CA's), which a [Node] does not
// have. The facade calls it at Start and after each CommissioningComplete,
// matter.js's node-online and fabric-added reactions. It never removes an
// entry, from a fabric it no longer grants either (see GrantFabric).
func (n *Node) ensureRequestorACL(ctx context.Context) {
	if n.cfg.WebRTCRequestor == nil || n.cfg.WebRTCRequestor.GrantFabric == nil {
		return
	}
	grant := n.cfg.WebRTCRequestor.GrantFabric
	topo := n.br.Topology()
	if topo == nil {
		return
	}
	var endpoints []uint16
	for _, ep := range topo.Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			if _, ok := srv.(*camera.Requestor); ok {
				endpoints = append(endpoints, ep.ID)
			}
		}
	}
	if len(endpoints) == 0 {
		return
	}
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		// No entry is written until the next Start or
		// CommissioningComplete: a granted camera refused meanwhile is
		// refused for this (review II, R2-C2-err-2456).
		n.logger.Warn("webrtc.requestor.acl_list_failed", slog.Any("err", err))
		return
	}
	if len(fabrics) == 0 { // :59-62
		return
	}
	for i := range fabrics {
		if !grant(fabricInfoOf(fabrics[i])) {
			continue
		}
		for _, epID := range endpoints {
			n.ensureRequestorACLOn(ctx, fabrics[i].FabricIndex, epID)
		}
	}
}

// ensureRequestorACLOn adds the requestor's Operate/CASE entry to
// fabricIndex's ACL unless it is there (the duplicate check :67-79, the
// entry :81-91), under the AccessControl server's write lock
// (addLocalOperateEntry).
func (n *Node) ensureRequestorACLOn(ctx context.Context, fabricIndex uint8, epID uint16) {
	if !n.addLocalOperateEntry(ctx, fabricIndex, epID, camera.RequestorClusterID, "webrtc.requestor.acl") {
		return
	}
	// After addLocalOperateEntry returned: the write lock is released, as
	// NotifyLocalEntryInstalledOn requires (review II, R5-par-5003).
	n.root.AccessControl.NotifyLocalEntryInstalledOn(ctx, fabricIndex)
	n.logger.Info("webrtc.requestor.acl_added", slog.Int("fabric_index", int(fabricIndex)), slog.Int("endpoint", int(epID)))
}
