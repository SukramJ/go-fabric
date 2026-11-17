// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"

	"github.com/SukramJ/go-fabric/internal/facadeseam"
	"github.com/SukramJ/go-fabric/store"
)

// addLocalOperateEntry adds an Operate entry for CASE, no subject, one
// target (epID, cluster) to fabricIndex's ACL unless the ACL holds one —
// the entry the node writes for a served WebRtcTransportRequestor and OTA
// provider. The read of the fabric's list, the comparison and the store
// run under the AccessControl server's write lock, the lock a
// controller's ACL write holds across the same three steps
// (internal/facadeseam.WithACLWriteLock; review II, R2-P6-sec-2401). A
// fabric the store no longer holds when the lock is
// taken gets nothing. Reports whether the entry was stored; event names
// the log lines (<event>_list, <event>_add).
func (n *Node) addLocalOperateEntry(ctx context.Context, fabricIndex uint8, epID uint16, cluster uint32, event string) bool {
	var added bool
	run := func() { added = n.addLocalOperateEntryLocked(ctx, fabricIndex, epID, cluster, event) }
	if !facadeseam.WithACLWriteLock(n.root.AccessControl, run) {
		// Not reached: the root's AccessControl is always a
		// *core.AccessControl. Fail closed — no write without the lock.
		n.logger.Error(event+"_lock", slog.String("err", "the AccessControl server's write lock is unavailable"))
		return false
	}
	return added
}

// addLocalOperateEntryLocked is addLocalOperateEntry's body, run under the
// AccessControl server's write lock.
func (n *Node) addLocalOperateEntryLocked(ctx context.Context, fabricIndex uint8, epID uint16, cluster uint32, event string) bool {
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		n.logger.Warn(event+"_list", slog.Any("err", err))
		return false
	}
	held := false
	for i := range fabrics {
		if fabrics[i].FabricIndex == fabricIndex {
			held = true
			break
		}
	}
	if !held {
		return false
	}
	acl, err := n.st.ListACL(ctx, fabricIndex)
	if err != nil {
		n.logger.Warn(event+"_list", slog.Any("err", err))
		return false
	}
	for _, e := range acl {
		if e.Privilege == store.PrivilegeOperate && e.AuthMode == store.AuthModeCASE && len(e.Subjects) == 0 &&
			len(e.Targets) == 1 && e.Targets[0].Endpoint != nil && *e.Targets[0].Endpoint == epID &&
			e.Targets[0].Cluster != nil && *e.Targets[0].Cluster == cluster {
			return false
		}
	}
	ep, cl := epID, cluster
	entry := store.ACLEntry{
		FabricIndex: fabricIndex, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeCASE,
		Targets: []store.ACLTarget{{Endpoint: &ep, Cluster: &cl}},
	}
	if err := n.st.ReplaceACL(ctx, fabricIndex, append(acl, entry)); err != nil {
		n.logger.Warn(event+"_add", slog.Any("err", err))
		return false
	}
	return true
}
