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
// otherwise set, the cluster's DataVersion advanced, persisted where a
// controller's written label is (the store's basic_information.node_label)
// and reported to the subscriptions that cover it and to
// [Observers].OnAttributeChanged with contract.OriginHostSet.
//
// It works before [Node.Start] as well: the label is the one the node
// serves when it starts. Either way it is persisted, so the next [New] on
// the same store restores it — over [Config].BasicInformation.NodeLabel,
// as it restores a controller's written label; that restore at New is not
// a change and reports nothing. A controller's later write wins in turn,
// and reaches the observers with contract.OriginControllerWrite. The
// node's UniqueID does not follow the label (quality F; see
// [BasicInformation].UniqueID).
//
// A bridged device's NodeLabel is device.Device.Rename's. Mirrors matter.js,
// whose host assigns state.basicInformation.nodeLabel on the server node
// and whose datasource commits it as it commits a controller's write
// (mattercore.BasicInformation.WriteNodeLabel cites the lines).
func (n *Node) SetNodeLabel(ctx context.Context, label string) error {
	return n.root.BasicInformation.WriteNodeLabel(ctx, label)
}
