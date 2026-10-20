// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The guard over this host's root-endpoint wiring.
//
// The cluster servers come from the module and are tested there; what the
// module cannot test is the wiring between them — the hooks the node closes
// on the root, the providers it installs when it starts. A hook nobody
// wires is indistinguishable from a wired one on every read, so each test
// here drives the node as main() assembles it and asserts the effect.
package main

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// testRootClusters builds endpoint 0 exactly as main() does — the node
// over a throw-away database and the CSA test attestation chain — and
// returns its servers in mount order and their handles.
func testRootClusters(t *testing.T) (servers []contract.ClusterServer, root *node.RootClusters) {
	t.Helper()
	n := testNode(t, newFleet(), filepath.Join(t.TempDir(), "reference-bridge.db"))
	return n.Root().Servers(), n.Root()
}

// TestCommissioningCompleteClearsThePendingCredentialState drives the
// sequence a commissioner leaves behind: a window armed, a CSR issued,
// CommissioningComplete accepted — then the auto-arm a later PASE
// establishment triggers. After Complete, OperationalCredentials must hold
// no pending state: the next AddNOC has to answer MissingCsr, not consume
// the CSR (and, in the real flow, the installed fabric index) the finished
// commissioning left behind. Left pending, that index is what a lapsed
// auto-armed window reverts — deleting the committed fabric.
func TestCommissioningCompleteClearsThePendingCredentialState(t *testing.T) {
	_, root := testRootClusters(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const fabric = 1
	if err := root.GeneralCommissioning.ArmFailSafeFor(ctx, 60, fabric); err != nil {
		t.Fatalf("ArmFailSafeFor: %v", err)
	}
	if _, err := root.OperationalCredentials.MatterInvoke(ctx, 0x04, mattercore.CSRRequest{CSRNonce: make([]byte, 32)}); err != nil {
		t.Fatalf("CSRRequest: %v", err)
	}

	raw, err := root.GeneralCommissioning.MatterInvoke(im.WithFabricFilter(ctx, false, fabric), 0x04, nil)
	if err != nil {
		t.Fatalf("CommissioningComplete: %v", err)
	}
	if resp, ok := raw.(mattercore.CommissioningCompleteResponse); !ok || resp.ErrorCode != mattercore.CommissioningErrorOK {
		t.Fatalf("CommissioningComplete = %#v, want ErrorCode OK", raw)
	}

	// The window a later Pake3 arms on its own — the path that fires no
	// OnFailSafeArmed and so clears nothing by itself.
	root.GeneralCommissioning.AutoArmOnPaseEstablished(ctx)

	raw, err = root.OperationalCredentials.MatterInvoke(ctx, 0x06, mattercore.AddNOCRequest{NOCValue: []byte{0x15}, IPKValue: make([]byte, 16), AdminVendorID: 0xFFF1})
	if err != nil {
		t.Fatalf("AddNOC: %v", err)
	}
	resp, ok := raw.(mattercore.NOCResponse)
	if !ok {
		t.Fatalf("AddNOC returned %T, want NOCResponse", raw)
	}
	if resp.StatusCode != mattercore.NOCStatusMissingCsr {
		t.Fatalf("AddNOC after CommissioningComplete = status %d (%q), want MissingCsr (%d): the finished "+
			"commissioning's pending state survived Complete — SetOnCommissioningComplete is not wired to ClearPendingState",
			resp.StatusCode, resp.DebugText, mattercore.NOCStatusMissingCsr)
	}
}

// TestRootPartsListNamesEveryDescendantEndpoint reads the root
// Descriptor's PartsList off the mounted cluster and requires the
// full-family shape: every endpoint below the root, not the aggregator
// alone. The aggregator's list is held to the same rule over the bridged
// endpoints.
func TestRootPartsListNamesEveryDescendantEndpoint(t *testing.T) {
	// The node mounts the root and the Aggregator and installs both
	// PartsList providers when it starts.
	_, br := startFleetBridge(t)

	topology := br.Topology()
	if topology == nil {
		t.Fatal("bridge has no topology after Start")
	}
	var wantRoot, wantAggregator []uint16
	for _, ep := range topology.Endpoints {
		if ep == nil || ep.IsRoot() {
			continue
		}
		wantRoot = append(wantRoot, ep.ID)
		if !ep.IsAggregator() {
			wantAggregator = append(wantAggregator, ep.ID)
		}
	}
	slices.Sort(wantRoot)
	slices.Sort(wantAggregator)
	if len(wantAggregator) < 2 {
		t.Fatalf("the fleet mounted only %d bridged endpoints; the test needs several to tell a full-family list from a tree", len(wantAggregator))
	}

	if got := partsList(t, topology.FindByID(0)); !slices.Equal(got, wantRoot) {
		t.Errorf("root PartsList = %v, want every descendant %v (root-node.element.ts:17 composition full-family)", got, wantRoot)
	}
	if got := partsList(t, topology.FindByID(1)); !slices.Equal(got, wantAggregator) {
		t.Errorf("aggregator PartsList = %v, want every bridged endpoint %v", got, wantAggregator)
	}
}

// partsList reads Descriptor.PartsList off the cluster server mounted on ep.
func partsList(t *testing.T, ep *endpoint.Endpoint) []uint16 {
	t.Helper()
	if ep == nil {
		t.Fatal("endpoint is not in the topology")
	}
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv == nil || srv.MatterClusterID() != 0x001D {
			continue
		}
		raw, ok := srv.MatterRead(0x0003)
		if !ok {
			t.Fatalf("endpoint %d: Descriptor.PartsList unreadable", ep.ID)
		}
		ids, ok := raw.([]uint16)
		if !ok {
			t.Fatalf("endpoint %d: Descriptor.PartsList is %T, want []uint16", ep.ID, raw)
		}
		return ids
	}
	t.Fatalf("endpoint %d mounts no Descriptor", ep.ID)
	return nil
}

// TestRootMountsGroupcastWithTheAuxiliaryACL: the bridged lights and plugs
// require the RootNode GroupcastListenerCond in Matter 1.6.1, so the root
// mounts Groupcast — Listener, Sender and PerGroup — and AccessControl advertises
// the Auxiliary feature the Listener feature needs (matter.js
// ServerNode.RootEndpoint: GroupcastServer + AccessControlServer.with(
// "Extension", "Auxiliary")). The Descriptor's ServerList names it.
func TestRootMountsGroupcastWithTheAuxiliaryACL(t *testing.T) {
	t.Parallel()
	servers, _ := testRootClusters(t)
	features := map[uint32]uint32{}
	for _, srv := range servers {
		if v, ok := srv.MatterRead(0xFFFC); ok {
			if fm, isU32 := v.(uint32); isU32 {
				features[srv.MatterClusterID()] = fm
			}
		}
	}
	if fm, ok := features[mattercore.GroupcastClusterID]; !ok || fm != 0x7 {
		t.Errorf("Groupcast FeatureMap = %#x (mounted %v), want Listener|Sender|PerGroup 0x7", fm, ok)
	}
	if fm := features[0x001F]; fm != 0x5 {
		t.Errorf("AccessControl FeatureMap = %#x, want Extension|Auxiliary 0x5", fm)
	}
	for _, srv := range servers {
		if srv.MatterClusterID() != 0x001D {
			continue
		}
		raw, _ := srv.MatterRead(0x0001)
		if list, _ := raw.([]uint32); !slices.Contains(list, mattercore.GroupcastClusterID) {
			t.Errorf("root ServerList %v does not name Groupcast", list)
		}
	}
}

// TestBlindPositionSurvivesARestart: the blind persists every position it
// reaches and a new blind on the same database starts there (matter.js
// keeps WindowCovering state non-volatile; TC-WNCV-4.5).
func TestBlindPositionSurvivesARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "reference-bridge.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	saved := make(chan struct{}, 16)
	first := newDemoBlind("blind")
	restoreBlind(ctx, st, first, slog.Default())
	persist := first.persist
	first.persist = func(pos uint16) { persist(pos); saved <- struct{}{} }
	srv := first.servers()[0]
	if _, err := srv.MatterInvoke(ctx, wire.WindowCoveringCmdGoToLiftPercentage, uint16(9000)); err != nil {
		t.Fatalf("GoToLiftPercentage: %v", err)
	}
	deadline := time.After(20 * time.Second)
	for {
		if v, _ := srv.MatterRead(wire.WindowCoveringAttrCurrentPositionLiftPercent100ths); v == uint16(9000) {
			break
		}
		select {
		case <-saved:
		case <-deadline:
			t.Fatal("the blind did not reach 9000")
		}
	}
	second := newDemoBlind("blind")
	restoreBlind(ctx, st, second, slog.Default())
	if v, _ := second.servers()[0].MatterRead(wire.WindowCoveringAttrCurrentPositionLiftPercent100ths); v != uint16(9000) {
		t.Fatalf("restarted blind at %v, want 9000", v)
	}
}
