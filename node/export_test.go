// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/jointfabric"
)

// RemoveFabricBindings is the node's removal of a fabric's Binding entries,
// what OperationalCredentials' RemoveFabric runs (fabricRemoved).
func RemoveFabricBindings(n *Node, fabricIndex uint8) { n.removeFabricBindings(fabricIndex) }

// Reassemblies is the number of reassemblies the Aggregator completed.
func Reassemblies(a *Aggregator) int64 { return a.reassemblies.Load() }

// CommissionerReachReports drives the commissioner reach's reads of the
// node nodeID of fabric fabricIndex over n's client stack — its
// ReadAdministratorFabricIndex on adminEndpoint, ReadFabrics,
// ReadTrustedRootCertificates and ReadNOCs, in that order — and returns
// the report each received, with each read's error.
func CommissionerReachReports(ctx context.Context, n *Node, fabricIndex uint8, nodeID uint64, adminEndpoint uint16) (reports []*interaction.Report, errs []error) {
	r := commissionerReach{n: n, observe: func(rep *interaction.Report) { reports = append(reports, rep) }}
	p := jointfabric.CommissionerPeer{FabricIndex: fabricIndex, NodeID: nodeID}
	_, _, err := r.ReadAdministratorFabricIndex(ctx, p, adminEndpoint)
	errs = append(errs, err)
	_, err = r.ReadFabrics(ctx, p)
	errs = append(errs, err)
	_, err = r.ReadTrustedRootCertificates(ctx, p)
	errs = append(errs, err)
	_, err = r.ReadNOCs(ctx, p)
	errs = append(errs, err)
	return reports, errs
}
