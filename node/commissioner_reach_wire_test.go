// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// TestCommissionerReachReadsConcretePaths (#429): every read of the node's
// commissioner reach — its production methods, driven through the test
// seam that captures their reports — goes out as a concrete path — endpoint, cluster and
// attribute flagged — so its report carries the one attribute it names:
// Fabrics, TrustedRootCertificates and NOCs a value each, and the
// AdministratorFabricIndex of a peer that serves no Joint Fabric
// Administrator one status (UNSUPPORTED_CLUSTER), as connectedhomeip's
// JCMCommissionee reads each attribute by its type on one endpoint
// (JCMCommissionee.cpp:46-69 at 6170af84). A path without the flags is a
// wildcard on the wire, the whole node in the report.
func TestCommissionerReachReadsConcretePaths(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	withClient := func(cfg *node.Config) { cfg.Client = &node.ClientConfig{} }
	commissioner := site.AddDevice(fabrictest.Topology{})
	reader := site.AddDevice(fabrictest.Topology{Configure: withClient})
	c := site.AddController(fabrictest.ControllerOptions{})
	pairC, err := c.Commission(ctx, commissioner, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatalf("commission the commissioner: %v", err)
	}
	pairR, err := c.Commission(ctx, reader, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatalf("commission the reader: %v", err)
	}
	if err := commissioner.Node().Store().ReplaceACL(ctx, 1, []store.ACLEntry{{
		FabricIndex: 1, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{c.Fabric().RootNodeID, pairR.Address.NodeID},
	}}); err != nil {
		t.Fatal(err)
	}
	reports, errs := node.CommissionerReachReports(ctx, reader.Node(), pairR.Address.FabricIndex, pairC.Address.NodeID, 0)
	// The peer serves no Joint Fabric Administrator: the first read's
	// status is its error; the other three succeed.
	if errs[0] == nil {
		t.Error("ReadAdministratorFabricIndex of a peer without the cluster succeeded")
	}
	for i, err := range errs[1:] {
		if err != nil {
			t.Errorf("read #%d: %v", i+1, err)
		}
	}
	want := []struct {
		cluster, attr uint32
		status        bool
	}{
		{jfadef.ClusterID, jfadef.AttrAdministratorFabricIndex, true},
		{opcreds.ClusterID, opcreds.AttrFabrics, false},
		{opcreds.ClusterID, opcreds.AttrTrustedRootCertificates, false},
		{opcreds.ClusterID, opcreds.AttrNocs, false},
	}
	if len(reports) != len(want) {
		t.Fatalf("%d reports, want %d", len(reports), len(want))
	}
	for i, w := range want {
		rep := reports[i]
		if len(rep.Attributes)+len(rep.Statuses) != 1 {
			t.Errorf("read of 0x%04X/0x%04X: %d values and %d statuses, want the one attribute", w.cluster, w.attr, len(rep.Attributes), len(rep.Statuses))
			continue
		}
		if w.status {
			if s := rep.Statuses[0]; len(rep.Statuses) != 1 || s.Path.Cluster != w.cluster || s.Path.Attribute != w.attr || s.Status != im.StatusUnsupportedCluster {
				t.Errorf("read of 0x%04X/0x%04X: %+v, want UNSUPPORTED_CLUSTER", w.cluster, w.attr, rep.Statuses)
			}
			continue
		}
		if _, ok := rep.Value(0, w.cluster, w.attr); !ok || len(rep.Attributes) != 1 {
			t.Errorf("read of 0x%04X/0x%04X: %d values, %+v statuses, want its value", w.cluster, w.attr, len(rep.Attributes), rep.Statuses)
		}
	}
}
