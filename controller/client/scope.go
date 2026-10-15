// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import "github.com/SukramJ/go-fabric/im"

// scope tells whether an endpoint and cluster are in scope of a read —
// matter.js ReadScope (packages/protocol/src/action/client/ReadScope.ts:13-52
// at 0ceb202d).
type scope struct {
	// isRelevant: the read covers some of the cluster, so a version filter
	// for it applies (ReadScope.ts:14-19, :40-43).
	isRelevant func(ep uint16, cluster uint32) bool
	// isWildcard: the read covers all of the cluster's attributes, so the
	// report's version is the cluster's (ReadScope.ts:21-26, :45-48).
	isWildcard func(ep uint16, cluster uint32) bool
	// fabricFiltered: the read's data was filtered by fabric —
	// isFabricFiltered ?? true (ReadScope.ts:50).
	fabricFiltered bool
}

// newScope is the scope of a read of paths — ReadScope(read)
// (ReadScope.ts:38-52), the testers built at once rather than on first use.
func newScope(paths []im.ConcreteAttributePath, noFabricFilter bool) scope {
	return scope{
		isRelevant:     scopeTester(paths, false),
		isWildcard:     scopeTester(paths, true),
		fabricFiltered: !noFabricFilter,
	}
}

// scopeTester is generateScopeTester (ReadScope.ts:54-106): all is the
// "all" requirement, any the "any" one.
func scopeTester(paths []im.ConcreteAttributePath, all bool) func(ep uint16, cluster uint32) bool {
	if len(paths) == 0 {
		return isNever
	}
	var wildcardEndpointClusters map[uint32]bool
	var specificEndpointClusters map[uint16]map[uint32]bool
	wholeEndpoint := map[uint16]bool{}
	for _, p := range paths {
		// A path naming an attribute says nothing of "all" (:63-66).
		if p.HasAttribute && all {
			continue
		}
		if !p.HasEndpoint {
			// The full wildcard short-circuits (:69-73).
			if !p.HasCluster {
				return isAlways
			}
			// A cluster across every endpoint (:76).
			if wildcardEndpointClusters == nil {
				wildcardEndpointClusters = map[uint32]bool{}
			}
			wildcardEndpointClusters[p.Cluster] = true
			continue
		}
		// An endpoint with every cluster (:81-84).
		if !p.HasCluster {
			wholeEndpoint[p.Endpoint] = true
			continue
		}
		// An endpoint and a cluster without an attribute (:86-96).
		if !p.HasAttribute {
			if wholeEndpoint[p.Endpoint] {
				continue
			}
			if specificEndpointClusters == nil {
				specificEndpointClusters = map[uint16]map[uint32]bool{}
			}
			if specificEndpointClusters[p.Endpoint] == nil {
				specificEndpointClusters[p.Endpoint] = map[uint32]bool{}
			}
			specificEndpointClusters[p.Endpoint][p.Cluster] = true
		}
	}
	return func(ep uint16, cluster uint32) bool {
		if wildcardEndpointClusters[cluster] {
			return true
		}
		return wholeEndpoint[ep] || specificEndpointClusters[ep][cluster]
	}
}

func isNever(uint16, uint32) bool { return false }

func isAlways(uint16, uint32) bool { return true }
