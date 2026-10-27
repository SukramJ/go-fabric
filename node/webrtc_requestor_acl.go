// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/store"
)

// ensureRequestorACL lets the camera that signals a WebRTC stream reach
// the controller's WebRtcTransportRequestor: on the first fabric, an
// Operate entry for CASE, no subject, the requestor's endpoint and
// cluster — added for each endpoint that serves the kit's requestor
// ([camera.Requestor]) unless the fabric's ACL has one. Without a fabric
// nothing happens; the facade calls it at Start and after each
// CommissioningComplete, matter.js's node-online and fabric-added
// reactions. Mirrors matter.js
// packages/node/src/behaviors/web-rtc-transport-requestor/
// WebRtcTransportRequestorServer.ts:45-94 (initialize, #nodeOnline:
// fabricAuthority.fabrics[0] at :58, the duplicate check :67-79, the entry
// :81-91).
func (n *Node) ensureRequestorACL(ctx context.Context) {
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
	if err != nil || len(fabrics) == 0 { // :59-62
		return
	}
	first := fabrics[0].FabricIndex
	for _, epID := range endpoints {
		n.ensureRequestorACLOn(ctx, first, epID)
	}
}

// ensureRequestorACLOn adds the requestor's Operate/CASE entry to
// fabricIndex's ACL unless it is there.
func (n *Node) ensureRequestorACLOn(ctx context.Context, fabricIndex uint8, epID uint16) {
	acl, err := n.st.ListACL(ctx, fabricIndex)
	if err != nil {
		n.logger.Warn("webrtc.requestor.acl_list", slog.Any("err", err))
		return
	}
	cluster := camera.RequestorClusterID
	for _, e := range acl { // :67-79
		if e.Privilege == store.PrivilegeOperate && e.AuthMode == store.AuthModeCASE && len(e.Subjects) == 0 &&
			len(e.Targets) == 1 && e.Targets[0].Endpoint != nil && *e.Targets[0].Endpoint == epID &&
			e.Targets[0].Cluster != nil && *e.Targets[0].Cluster == cluster {
			return
		}
	}
	ep := epID
	entry := store.ACLEntry{
		FabricIndex: fabricIndex, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeCASE,
		Targets: []store.ACLTarget{{Endpoint: &ep, Cluster: &cluster}},
	}
	if err := n.st.ReplaceACL(ctx, fabricIndex, append(acl, entry)); err != nil { // :81-91
		n.logger.Warn("webrtc.requestor.acl_add", slog.Any("err", err))
		return
	}
	n.root.AccessControl.NotifyLocalEntryInstalled()
	n.logger.Info("webrtc.requestor.acl_added", slog.Int("fabric_index", int(fabricIndex)), slog.Int("endpoint", int(epID)))
}
