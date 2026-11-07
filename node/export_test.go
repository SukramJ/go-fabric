// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/jointfabric"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/im"
)

// RemoveFabricBindings is the node's removal of a fabric's Binding entries,
// what OperationalCredentials' RemoveFabric runs (fabricRemoved).
func RemoveFabricBindings(n *Node, fabricIndex uint8) { n.removeFabricBindings(fabricIndex) }

// Reassemblies is the number of reassemblies the Aggregator completed.
func Reassemblies(a *Aggregator) int64 { return a.reassemblies.Load() }

// CommissionerReachReports are the reports of the commissioner reach's
// reads of the node nodeID of fabric fabricIndex — AdministratorFabricIndex
// on adminEndpoint, then Fabrics, TrustedRootCertificates and NOCs — over
// n's client stack, each with the path the reach builds.
func CommissionerReachReports(ctx context.Context, n *Node, fabricIndex uint8, nodeID uint64, adminEndpoint uint16) ([]*interaction.Report, error) {
	r := commissionerReach{n}
	p := jointfabric.CommissionerPeer{FabricIndex: fabricIndex, NodeID: nodeID}
	var out []*interaction.Report
	for _, path := range []im.ConcreteAttributePath{
		administratorFabricIndexPath(adminEndpoint),
		opCredsPath(opcreds.AttrFabrics), opCredsPath(opcreds.AttrTrustedRootCertificates), opCredsPath(opcreds.AttrNocs),
	} {
		rep, err := r.readReport(ctx, p, path)
		if err != nil {
			return out, err
		}
		out = append(out, rep)
	}
	return out, nil
}
