// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// The guard over this host's root-endpoint wiring.
//
// The cluster servers come from the module and are tested there; what the
// module cannot test is the wiring a host has to do between them — the
// hooks buildRootClusters closes, the providers attachPartsListProviders
// installs. A hook nobody wires is indistinguishable from a wired one on
// every read, so each test here drives the real constructor and asserts
// the effect.
package main

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/attestation"
	"github.com/SukramJ/go-fabric/store"
)

// testRootClusters builds endpoint 0 exactly as main() does, over a
// throw-away database and the CSA test attestation chain.
func testRootClusters(t *testing.T) (servers []contract.ClusterServer, refs rootRefs) {
	t.Helper()
	ctx := context.Background()
	db, err := openDB(ctx, filepath.Join(t.TempDir(), "reference-bridge.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	chain, err := attestation.BuildTestChain(testVendorID, testProductID)
	if err != nil {
		t.Fatalf("BuildTestChain: %v", err)
	}
	identity := bridgeIdentity{vendorID: testVendorID, productID: testProductID, nodeLabel: "wiring-test", serialNumber: "TEST-0001"}
	servers, refs, err = buildRootClusters(identity, store.New(db), chain,
		func(context.Context, uint8, uint64, uint64, []byte) {})
	if err != nil {
		t.Fatalf("buildRootClusters: %v", err)
	}
	return servers, refs
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
	_, refs := testRootClusters(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const fabric = 1
	if err := refs.generalCom.ArmFailSafeFor(ctx, 60, fabric); err != nil {
		t.Fatalf("ArmFailSafeFor: %v", err)
	}
	if _, err := refs.opCreds.MatterInvoke(ctx, 0x04, mattercore.CSRRequest{CSRNonce: make([]byte, 32)}); err != nil {
		t.Fatalf("CSRRequest: %v", err)
	}

	raw, err := refs.generalCom.MatterInvoke(im.WithFabricFilter(ctx, false, fabric), 0x04, nil)
	if err != nil {
		t.Fatalf("CommissioningComplete: %v", err)
	}
	if resp, ok := raw.(mattercore.CommissioningCompleteResponse); !ok || resp.ErrorCode != mattercore.CommissioningErrorOK {
		t.Fatalf("CommissioningComplete = %#v, want ErrorCode OK", raw)
	}

	// The window a later Pake3 arms on its own — the path that fires no
	// OnFailSafeArmed and so clears nothing by itself.
	refs.generalCom.AutoArmOnPaseEstablished(ctx)

	raw, err = refs.opCreds.MatterInvoke(ctx, 0x06, mattercore.AddNOCRequest{NOCValue: []byte{0x15}, IPKValue: make([]byte, 16)})
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
	_, br := startFleetBridge(t)
	rootServers, _ := testRootClusters(t)
	br.AttachRootClusters(rootServers)
	aggregatorServers, err := buildAggregatorClusters()
	if err != nil {
		t.Fatalf("buildAggregatorClusters: %v", err)
	}
	br.AttachAggregatorClusters(aggregatorServers)
	if err := attachPartsListProviders(br); err != nil {
		t.Fatalf("attachPartsListProviders: %v", err)
	}

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
