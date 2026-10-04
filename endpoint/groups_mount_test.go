// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/store"
)

// nopGroupStore is a group store with one fabric and nothing else.
type nopGroupStore struct{}

func (nopGroupStore) GetFabric(context.Context, uint8) (store.FabricRecord, error) {
	return store.FabricRecord{FabricIndex: 1}, nil
}

func (nopGroupStore) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	return nil, nil
}

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

// deviceTypeSource is a source of a given device type with given servers.
type deviceTypeSource struct {
	dt      uint16
	servers []contract.ClusterServer
}

func (d deviceTypeSource) MatterDeviceType() uint16                       { return d.dt }
func (d deviceTypeSource) MatterClusterServers() []contract.ClusterServer { return d.servers }

func clusterIDs(servers []contract.ClusterServer) []uint32 {
	ids := make([]uint32, 0, len(servers))
	for _, s := range servers {
		ids = append(ids, s.MatterClusterID())
	}
	return ids
}

func countCluster(servers []contract.ClusterServer, id uint32) (n int, stack bool) {
	for _, s := range servers {
		if s.MatterClusterID() == id {
			n++
			_, stack = s.(*mattercore.Groups)
		}
	}
	return n, stack
}

// TestGroupsServerIsMountedWhereTheDeviceTypeMandatesIt pins the mounting
// rule: with the node's group state configured, an OnOffLight / plug
// endpoint serves the stack's Groups server whether or not its source
// supplied one (a supplied one is replaced), a device type that does not
// mandate Groups gets it only when its source asked for it, and without
// group state the source's set is served unchanged.
func TestGroupsServerIsMountedWhereTheDeviceTypeMandatesIt(t *testing.T) {
	t.Parallel()
	mgr, err := groups.NewManager(nopGroupStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	onoff := fakeServer{id: 0x0006}
	stub := fakeServer{id: mattercore.GroupsClusterID}

	cases := []struct {
		name      string
		dt        uint16
		servers   []contract.ClusterServer
		groups    *groups.Manager
		wantCount int
		wantStack bool
	}{
		{"OnOffLight without a supplied Groups", 0x0100, []contract.ClusterServer{onoff}, mgr, 1, true},
		{"OnOffPlugInUnit with the stub replaced", 0x010A, []contract.ClusterServer{onoff, stub}, mgr, 1, true},
		{"WindowCovering (Groups conditional) without one", 0x0202, []contract.ClusterServer{fakeServer{id: 0x0102}}, mgr, 0, false},
		{"WindowCovering that asked for Groups", 0x0202, []contract.ClusterServer{fakeServer{id: 0x0102}, stub}, mgr, 1, true},
		{"no group state keeps the source's set", 0x0100, []contract.ClusterServer{onoff, stub}, nil, 1, false},
		{"no group state, nothing supplied", 0x0100, []contract.ClusterServer{onoff}, nil, 0, false},
	}
	for _, tc := range cases {
		ep := &Endpoint{
			ID: 7, DeviceType: tc.dt, FriendlyName: "Lamp", SourceKey: StringKey("lamp"),
			Source: deviceTypeSource{dt: tc.dt, servers: tc.servers}, groups: tc.groups,
		}
		servers := ClusterServers(ep)
		n, stack := countCluster(servers, mattercore.GroupsClusterID)
		if n != tc.wantCount || (n > 0 && stack != tc.wantStack) {
			t.Errorf("%s: Groups servers = %d (stack=%v), want %d (stack=%v); clusters %v", tc.name, n, stack, tc.wantCount, tc.wantStack, clusterIDs(servers))
		}
		if tc.wantStack {
			if ids := clusterIDs(servers); len(ids) < 2 || ids[0] != 0x0003 || ids[1] != mattercore.GroupsClusterID {
				t.Errorf("%s: cluster order %v, want Identify then Groups first", tc.name, ids)
			}
			// The ServerList the Descriptor serves names it once.
			for _, srv := range servers {
				if d, ok := srv.(*mattercore.Descriptor); ok {
					v, _ := d.MatterRead(0x0001)
					list, _ := v.([]uint32)
					seen := 0
					for _, id := range list {
						if id == mattercore.GroupsClusterID {
							seen++
						}
					}
					if seen != 1 {
						t.Errorf("%s: ServerList %v names Groups %d times", tc.name, list, seen)
					}
				}
			}
		}
	}
}

// TestAssemblerStampsGroupState pins that the assembler hands its group
// state to every bridged endpoint it builds.
func TestAssemblerStampsGroupState(t *testing.T) {
	t.Parallel()
	mgr, _ := groups.NewManager(nopGroupStore{}, nil)
	a, err := New(&oneEndpointStore{}, Config{VendorID: 1, ProductID: 1, NodeLabel: "x", Groups: mgr}, nil)
	if err != nil {
		t.Fatal(err)
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{{
		StableKey: StringKey("k"), DeviceType: 0x0100, FriendlyName: "Lamp",
		Source: deviceTypeSource{dt: 0x0100, servers: []contract.ClusterServer{fakeServer{id: 0x0006}}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	bridged := topo.Bridged()
	if len(bridged) != 1 || bridged[0].groups != mgr {
		t.Fatalf("bridged endpoints %+v do not carry the group state", bridged)
	}
	if n, stack := countCluster(ClusterServers(bridged[0]), mattercore.GroupsClusterID); n != 1 || !stack {
		t.Fatal("assembled OnOffLight endpoint does not serve the stack's Groups server")
	}
}

// oneEndpointStore is a minimal identity store for the assembler test.
type oneEndpointStore struct{ rec *Record }

func (s *oneEndpointStore) GetEndpoint(context.Context, SourceKey) (Record, error) {
	if s.rec == nil {
		return Record{}, ErrNotFound
	}
	return *s.rec, nil
}

func (s *oneEndpointStore) UpsertEndpointAssigning(_ context.Context, rec Record) (uint16, error) {
	if rec.EndpointID == 0 {
		rec.EndpointID = 2
	}
	s.rec = &rec
	return rec.EndpointID, nil
}

func (s *oneEndpointStore) ListEndpoints(context.Context, string) ([]Record, error) {
	if s.rec == nil {
		return nil, nil
	}
	return []Record{*s.rec}, nil
}

func (s *oneEndpointStore) RemoveEndpoint(context.Context, SourceKey) error { return nil }
