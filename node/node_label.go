// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import "context"

// SetNodeLabel sets the node's own NodeLabel — the root BasicInformation's,
// the name a controller shows for the node — at run time, with a
// controller's write semantics: the label is held to the attribute's
// constraint (at most 32 bytes; empty is allowed) and refused with the
// status a controller's write gets (CONSTRAINT_ERROR, an
// im.StatusCodeError) — the server's error is returned unchanged — and
// otherwise persisted where a controller's written label is (the store's
// basic_information.node_label; a store failure is returned, wrapped, with
// nothing served changed), then set, the cluster's DataVersion advanced
// and reported to the subscriptions that cover it and to
// [Observers].OnAttributeChanged with contract.OriginHostSet.
//
// Before [Node.Start] the label is configuration: it is the one the node
// serves when it starts, and it is persisted, but no subscription exists
// yet and the observers are not told — the notifier is wired when the
// node starts. matter.js would emit `$Changed` on the behavior before the
// node is online; reporting only a running node's changes is the facade's
// decision. Either way the label is persisted, so the next [New] on the
// same store restores it — over [Config].BasicInformation.NodeLabel,
// as it restores a controller's written label; that restore at New is not
// a change and reports nothing. A controller's later write wins in turn,
// and reaches the observers with contract.OriginControllerWrite. The
// node's UniqueID does not follow the label (quality F; see
// [BasicInformation].UniqueID).
//
// A bridged device's NodeLabel is device.Device.Rename's. Mirrors matter.js,
// whose host assigns state.basicInformation.nodeLabel on the server node
// and whose datasource commits it as it commits a controller's write
// (mattercore.BasicInformation.SetNodeLabel cites the lines).
func (n *Node) SetNodeLabel(ctx context.Context, label string) error {
	return n.root.BasicInformation.SetNodeLabel(ctx, label)
}
