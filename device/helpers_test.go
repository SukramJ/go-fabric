// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"maps"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/store"
)

// testOptions are the Options a test device of a given name gets.
func testOptions(name string) Options {
	return Options{StableKey: endpoint.StringKey("test:" + name), Name: name}
}

// nopGroupStore is a group store with one fabric and nothing else, so the
// assembler mounts Groups and ScenesManagement where the type mandates
// them.
type nopGroupStore struct{}

func (nopGroupStore) GetFabric(context.Context, uint8) (store.FabricRecord, error) {
	return store.FabricRecord{FabricIndex: 1}, nil
}

func (nopGroupStore) ListFabrics(context.Context) ([]store.FabricRecord, error) { return nil, nil }

func (nopGroupStore) ListGroupKeySets(context.Context, uint8) ([]store.GroupKeySet, error) {
	return nil, nil
}

func (nopGroupStore) ListGroupKeyMappings(context.Context, uint8) ([]store.GroupKeyMapping, error) {
	return nil, nil
}

func (nopGroupStore) ListGroupTable(context.Context, uint8) ([]store.GroupTableEntry, error) {
	return nil, nil
}

func (nopGroupStore) UpsertGroupTableEntry(context.Context, store.GroupTableEntry) error { return nil }

func (nopGroupStore) RemoveGroupTableEntry(context.Context, uint8, uint16) error { return nil }

func (nopGroupStore) ListGroupcastGroups(context.Context, uint8) ([]store.GroupcastGroup, error) {
	return nil, nil
}

func (nopGroupStore) UpsertGroupcastGroup(context.Context, store.GroupcastGroup) error { return nil }

func (nopGroupStore) RemoveGroupcastGroup(context.Context, uint8, uint16) error { return nil }

// assemble runs devices through the endpoint assembler, as a host's
// snapshot does, with the node's group state configured.
func assemble(t *testing.T, devices ...*Device) *endpoint.Topology {
	t.Helper()
	mgr, err := groups.NewManager(nopGroupStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := endpointtest.AssemblerConfig()
	cfg.Groups = mgr
	a, err := endpoint.New(endpointtest.NewFakeStore(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	specs := make([]endpoint.Spec, 0, len(devices))
	for _, d := range devices {
		specs = append(specs, d.Spec())
	}
	topo, err := a.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "test", Endpoints: specs, ModelComplete: true}})
	if err != nil {
		t.Fatal(err)
	}
	return topo
}

// violationsOf returns the device-type violations of the bridged endpoints
// (the device and its parts), leaving the unpublished root and aggregator
// out.
func violationsOf(topo *endpoint.Topology) []endpoint.DeviceTypeViolation {
	var out []endpoint.DeviceTypeViolation
	all := endpoint.ValidateDeviceTypes(topo)
	for i := range all {
		if ep := topo.FindByID(all[i].Endpoint); ep != nil && (ep.IsRoot() || ep.IsAggregator()) {
			continue
		}
		out = append(out, all[i])
	}
	return out
}

// The kit's per-cluster statements by cluster id, for the tests that walk
// every device type or every cluster without naming its generated package:
// the untyped form a <cluster>def.Declare compiles to, merged the same way.
// There is no public form of these: a host declares the cluster.

// withFeatures states the FeatureMap selection of a cluster.
func withFeatures(clusterID, features uint32) Option {
	return clusterOption{decl: spec.ClusterDeclaration{Cluster: clusterID, Features: features, HasFeatures: true}, mount: true}
}

// withInitial states initial attribute values of a cluster.
func withInitial(clusterID uint32, values map[uint32]any) Option {
	initial := maps.Clone(values)
	if initial == nil {
		initial = map[uint32]any{}
	}
	return clusterOption{decl: spec.ClusterDeclaration{Cluster: clusterID, Initial: initial}}
}

// withElements states optional elements of a cluster.
func withElements(clusterID uint32, opts spec.Options) Option {
	return clusterOption{decl: spec.ClusterDeclaration{Cluster: clusterID, Elements: opts}, mount: true}
}

// withOptional mounts an optional cluster with its default server.
func withOptional(clusterID uint32) Option {
	return clusterOption{decl: spec.ClusterDeclaration{Cluster: clusterID}, mount: true}
}

// withServer states the host's own server of a cluster.
func withServer(srv contract.ClusterServer) Option {
	return clusterOption{decl: spec.ClusterDeclaration{Cluster: srv.MatterClusterID(), Server: srv}}
}
