// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

const (
	timeSyncRow = "BD-chip-TimeSynchronization-Features"
	threadRow   = "BD-Thread-NetComm-ThreadVariant"
)

// timeSyncFeatures are the four features the TimeSynchronization row
// declares (core.TimeSynchronizationAhead).
const timeSyncFeatures = tsdef.FeatureTimeZone | tsdef.FeatureNtpClient | tsdef.FeatureNtpServer | tsdef.FeatureTimeSyncClient

// aheadNode builds a node with the root's TimeSynchronization features
// (none when ts is nil) under p.
func aheadNode(t *testing.T, ts *node.TimeSynchronizationConfig, p surface.Policies) (*node.Node, error) {
	t.Helper()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "ahead.db")))
	cfg.TimeSynchronization, cfg.Surface = ts, p
	n, err := node.New(cfg)
	if err == nil {
		t.Cleanup(func() { _ = n.Stop(context.Background()) })
	}
	return n, err
}

func rootCluster(r surface.Report, id uint32) surface.Cluster {
	for _, c := range r.Effective.Root.Clusters {
		if c.ID == id {
			return c
		}
	}
	return surface.Cluster{}
}

func TestNodeAheadRows(t *testing.T) {
	want := []string{threadRow, timeSyncRow}
	slices.Sort(want)
	if got := node.AheadRows(); !slices.Equal(got, want) {
		t.Errorf("AheadRows() = %v, want %v", got, want)
	}
	if got := mattercore.TimeSynchronizationAhead.Row(surface.KindFeature, uint32(tsdef.FeatureNtpServer)); got != timeSyncRow {
		t.Errorf("NTPS is declared as %q", got)
	}
	if got := mattercore.NetworkCommissioningAhead.Row(surface.KindFeature, mattercore.NetworkCommFeatureThread); got != threadRow {
		t.Errorf("Thread is declared as %q", got)
	}
	if got := mattercore.NetworkCommissioningAhead.Row(surface.KindFeature, mattercore.NetworkCommFeatureEthernet); got != "" {
		t.Errorf("Ethernet is declared as %q", got)
	}
}

func TestNodeAheadTimeSynchronizationDisabled(t *testing.T) {
	for _, f := range []tsdef.Feature{tsdef.FeatureTimeZone, tsdef.FeatureNtpClient, tsdef.FeatureNtpServer, tsdef.FeatureTimeSyncClient} {
		_, err := aheadNode(t, &node.TimeSynchronizationConfig{Features: f}, surface.Policies{})
		if !errors.Is(err, surface.ErrExtensionNotEnabled) || !errors.Is(err, surface.ErrRefused) {
			t.Errorf("feature 0x%X: err = %v, want ErrExtensionNotEnabled", uint32(f), err)
			continue
		}
		if !strings.Contains(err.Error(), timeSyncRow) || !strings.Contains(err.Error(), "the root endpoint") {
			t.Errorf("feature 0x%X: error %q does not name the row and the root", uint32(f), err)
		}
	}
	// Another row enabled does not enable this one.
	if _, err := aheadNode(t, &node.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone},
		surface.Policies{Extensions: surface.NewExtensions(threadRow)}); !errors.Is(err, surface.ErrExtensionNotEnabled) {
		t.Errorf("with only %s enabled: %v", threadRow, err)
	}
}

// TestNodeAheadNotSelected: a node that selects no ahead element builds
// under the zero-value policies with no root decision, and its root is the
// one it has with every row enabled. The root's NetworkCommissioning is
// Ethernet: the Thread row's bit is never a candidate.
func TestNodeAheadNotSelected(t *testing.T) {
	for _, ts := range []*node.TimeSynchronizationConfig{nil, {}} {
		plain, err := aheadNode(t, ts, surface.Policies{})
		if err != nil {
			t.Fatal(err)
		}
		all, err := aheadNode(t, ts, surface.Policies{Extensions: surface.NewExtensions(node.AheadRows()...)})
		if err != nil {
			t.Fatal(err)
		}
		a, b := plain.Surface(), all.Surface()
		if len(a.Decisions) != 0 {
			t.Errorf("root decisions %+v, want none", a.Decisions)
		}
		if diff := surface.Diff(surface.Surface{Root: a.Effective.Root}, surface.Surface{Root: b.Effective.Root}); len(diff) != 0 {
			t.Errorf("enabling rows the node does not select changes the root: %v", diff)
		}
		if fm := rootCluster(a, netcommdef.ClusterID).FeatureMap; fm != mattercore.NetworkCommFeatureEthernet {
			t.Errorf("NetworkCommissioning FeatureMap 0x%X, want Ethernet", fm)
		}
	}
}

// TestNodeAheadTimeSynchronizationEnabled: with the row enabled the node
// builds, the root's FeatureMap carries the bits, each selected bit has a
// kept decision tagged with the row, and the cluster is the one the
// server serves built directly with the same features.
func TestNodeAheadTimeSynchronizationEnabled(t *testing.T) {
	p := surface.Policies{Extensions: surface.NewExtensions(timeSyncRow)}
	n, err := aheadNode(t, &node.TimeSynchronizationConfig{Features: timeSyncFeatures}, p)
	if err != nil {
		t.Fatal(err)
	}
	rep := n.Surface()
	c := rootCluster(rep, tsdef.ClusterID)
	if c.FeatureMap != uint32(timeSyncFeatures) {
		t.Errorf("FeatureMap 0x%X, want 0x%X", c.FeatureMap, uint32(timeSyncFeatures))
	}
	if len(rep.Decisions) != 4 {
		t.Fatalf("root decisions %+v, want one per feature", rep.Decisions)
	}
	for _, d := range rep.Decisions {
		if d.Endpoint != "" || d.Cluster != tsdef.ClusterID || d.Kind != surface.KindFeature ||
			d.Reference.Ahead != timeSyncRow || d.Verdict != surface.Keep || d.Mandatory || d.Origin != surface.OriginHostDeclared {
			t.Errorf("decision %+v", d)
		}
	}
	direct, err := mattercore.NewTimeSynchronizationWith(mattercore.TimeSynchronizationConfig{Features: timeSyncFeatures})
	if err != nil {
		t.Fatal(err)
	}
	if want := endpoint.ClusterSurface(direct); !reflect.DeepEqual(c, want) {
		t.Errorf("root TimeSynchronization\n got %+v\nwant %+v", c, want)
	}
	plain, err := aheadNode(t, nil, surface.Policies{})
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(c, rootCluster(plain.Surface(), tsdef.ClusterID)) {
		t.Error("the features change nothing on the root")
	}
}

// TestNodeAheadCustomRemove: a custom policy that removes a root feature
// takes it out of the TimeSynchronization the node builds.
func TestNodeAheadCustomRemove(t *testing.T) {
	p := surface.Policies{
		Extensions: surface.NewExtensions(timeSyncRow),
		Custom:     []surface.Policy{surfacetest.Remove(surface.KindFeature, surfacetest.Match{Cluster: tsdef.ClusterID, ID: uint32(tsdef.FeatureNtpServer)})},
	}
	ts := &node.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone | tsdef.FeatureNtpServer}
	n, err := aheadNode(t, ts, p)
	if err != nil {
		t.Fatal(err)
	}
	if fm := rootCluster(n.Surface(), tsdef.ClusterID).FeatureMap; fm != uint32(tsdef.FeatureTimeZone) {
		t.Errorf("FeatureMap 0x%X, want TZ alone", fm)
	}
	if ts.Features != tsdef.FeatureTimeZone|tsdef.FeatureNtpServer {
		t.Error("the host's configuration was changed")
	}
}

func TestAddRefusesTheReservedKey(t *testing.T) {
	ctx := context.Background()
	n := surfaceNode(t, surface.Policies{})
	agg := n.Aggregator()
	if err := agg.Add(ctx, light(t, node.AggregatorSurfaceKey, nil)); !errors.Is(err, node.ErrReservedKey) {
		t.Fatalf("Add with the reserved key: %v", err)
	}
	owner, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("owner")})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AddPart(light(t, node.AggregatorSurfaceKey, nil).Device); err != nil {
		t.Fatal(err)
	}
	if err := agg.Add(ctx, owner); !errors.Is(err, node.ErrReservedKey) {
		t.Fatalf("Add of an owner whose part has the reserved key: %v", err)
	}
	if err := agg.Add(ctx, light(t, "aggregator-1", nil)); err != nil {
		t.Fatal(err)
	}
}
