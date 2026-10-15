// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package binding

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/schema"
)

// The reasons a binding entry is dropped or waits, matter.js's log
// messages verbatim (BindingManager.ts at 0ceb202d, line of each).
const (
	reasonNodeXorGroup        = "Binding entry must have exactly one of node/group"                          // :261
	reasonGroupFabricUnknown  = "Group binding fabric unknown"                                               // :277
	reasonSelfNoEndpoint      = "Self-binding to non-existent endpoint"                                      // :285
	reasonSelfNoServer        = "Self-binding references cluster not installed as server on target endpoint" // :293
	reasonClientNoEndpoint    = "Client binding entry missing endpoint"                                      // :299
	reasonGroupWaitsForKey    = "Group binding waits for the fabric to hold a key for the group"             // :312
	reasonClientPeerFailed    = "Client binding peer registration failed"                                    // :335
	reasonEstablishedFailed   = "Binding established handler failed"                                         // :378
	reasonDormant             = "Binding entry waits until something observes binding.established"           // :404
	reasonGroupPeerFailed     = "Group binding peer registration failed"                                     // :463
	reasonInitiatingCASE      = "Initiating CASE session for bound peer"                                     // :593
	reasonPeerSetLookupFailed = "PeerSet lookup failed"                                                      // :604
	reasonRemovedFailed       = "Binding removed handler failed"                                             // :200, :238
	reasonEstablished         = "Binding established"                                                        // :367
	reasonRemoved             = "Binding removed"                                                            // :189, :227
	reasonClientStackDisabled = "client stack disabled"                                                      // Go-only: the node runs without its client stack
)

// ignoredEntryReason is why an entry resolves to no client of the source
// endpoint, phrased for the developer reading the log — matter.js
// ignoredEntryReason (BindingManager.ts:721-737), its four messages
// verbatim. unbindable are the clients the entry matches that are left
// out as not bindable; cluster names come from the schema, as matter.js's
// from its model.
func ignoredEntryReason(sourceEndpoint uint16, filterCluster uint32, hasFilter bool, unbindable []uint32) string {
	if len(unbindable) == 1 {
		return fmt.Sprintf("Ignoring binding entry for cluster %s: we never use this client through a binding", clusterName(unbindable[0]))
	}
	if len(unbindable) > 0 {
		names := make([]string, 0, len(unbindable))
		for _, c := range unbindable {
			names = append(names, clusterName(c))
		}
		return fmt.Sprintf("Ignoring binding entry for clusters %s: we never use these clients through a binding", strings.Join(names, ", "))
	}
	if !hasFilter {
		return fmt.Sprintf("Ignoring binding entry: endpoint %d has no client cluster declared", sourceEndpoint)
	}
	return fmt.Sprintf("Ignoring binding entry for cluster %s: endpoint %d has no client for this cluster declared", clusterName(filterCluster), sourceEndpoint)
}

// clusterName is the cluster's model name, else its id as matter.js prints
// it (`0x${id.toString(16)}`, BindingManager.ts:735).
func clusterName(id uint32) string {
	if n, ok := schema.ClusterName(id); ok {
		return n
	}
	return "0x" + strconv.FormatUint(uint64(id), 16)
}
