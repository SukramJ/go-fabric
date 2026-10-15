// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// RemoveFabricBindings is the node's removal of a fabric's Binding entries,
// what OperationalCredentials' RemoveFabric runs (fabricRemoved).
func RemoveFabricBindings(n *Node, fabricIndex uint8) { n.removeFabricBindings(fabricIndex) }
