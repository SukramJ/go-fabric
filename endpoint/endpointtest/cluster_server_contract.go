// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpointtest

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/contract"
)

// AssertClusterServerContract holds srv to [contract.ClusterServer]'s read
// contract — the check the module runs over its own servers, offered to a
// host for its own: every attribute srv lists ([contract.ClusterAttributeLister])
// answers a read ok (MatterRead, or MatterReadFiltered for a
// [contract.FabricScopedReader]); every attribute MatterReportable names is
// one it lists; FeatureMap, when answered, is a uint32 and ClusterRevision a
// uint16. The three global lists the dispatcher answers itself
// (AttributeList, AcceptedCommandList, GeneratedCommandList) are not read;
// every other listed attribute is — FeatureMap and ClusterRevision, which
// the dispatcher reads from srv, and a manufacturer-specific attribute
// included.
//
// tolerate lists the listed attributes srv knowingly answers no read of; a
// tolerated attribute that answers fails tb too, so the list cannot outlive
// its reason. It returns the listed attributes that answer no read,
// tolerated ones included.
func AssertClusterServerContract(tb testing.TB, srv contract.ClusterServer, tolerate ...uint32) []uint32 {
	tb.Helper()
	id := srv.MatterClusterID()
	reportable := srv.MatterReportable()
	listed := reportable
	if lister, ok := srv.(contract.ClusterAttributeLister); ok {
		listed = lister.MatterAttributes()
		for _, attr := range reportable {
			if !slices.Contains(listed, attr) {
				tb.Errorf("cluster 0x%04X reports attribute 0x%04X, which MatterAttributes does not list", id, attr)
			}
		}
	}
	if v, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap); ok {
		if _, isUint32 := v.(uint32); !isUint32 {
			tb.Errorf("cluster 0x%04X answers FeatureMap as %T, want uint32", id, v)
		}
	}
	if v, ok := srv.MatterRead(cluster.AttrGlobalClusterRevision); ok {
		if _, isUint16 := v.(uint16); !isUint16 {
			tb.Errorf("cluster 0x%04X answers ClusterRevision as %T, want uint16", id, v)
		}
	}
	var unanswered []uint32
	for _, attr := range listed {
		if dispatcherAnswered(attr) {
			continue
		}
		if answersRead(srv, attr) {
			if slices.Contains(tolerate, attr) {
				tb.Errorf("cluster 0x%04X attribute 0x%04X is tolerated as unanswered and answers a read: remove it", id, attr)
			}
			continue
		}
		unanswered = append(unanswered, attr)
		if !slices.Contains(tolerate, attr) {
			tb.Errorf("cluster 0x%04X lists attribute 0x%04X and answers no read of it", id, attr)
		}
	}
	return unanswered
}

// answersRead reports whether srv answers a read of attr ok, as the
// dispatcher reads it: the fabric-scoped read first, MatterRead otherwise.
func answersRead(srv contract.ClusterServer, attr uint32) bool {
	if fsr, ok := srv.(contract.FabricScopedReader); ok {
		if _, ok := fsr.MatterReadFiltered(context.Background(), attr); ok {
			return true
		}
	}
	_, ok := srv.MatterRead(attr)
	return ok
}

// dispatcherAnswered reports whether the dispatcher answers a read of the
// global attribute attr itself when the server lists it: the attribute
// list and the two command lists, built from the server's listers.
func dispatcherAnswered(attr uint32) bool {
	switch attr {
	case cluster.AttrGlobalAttributeList, cluster.AttrGlobalAcceptedCommandList, cluster.AttrGlobalGeneratedCommandList:
		return true
	}
	return false
}
