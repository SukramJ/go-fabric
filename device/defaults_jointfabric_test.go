// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/jointfabric"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
)

// fakeDatastoreSync answers every push and fetch empty.
type fakeDatastoreSync struct{}

func (fakeDatastoreSync) SyncEndpointGroupID(context.Context, uint64, dsdef.DatastoreEndpointGroupIDEntryStruct) error {
	return nil
}
func (fakeDatastoreSync) SyncNodeKeySet(context.Context, uint64, dsdef.DatastoreNodeKeySetEntryStruct) error {
	return nil
}
func (fakeDatastoreSync) SyncBinding(context.Context, uint64, dsdef.DatastoreEndpointBindingEntryStruct) error {
	return nil
}
func (fakeDatastoreSync) SyncBindings(context.Context, uint64, []dsdef.DatastoreEndpointBindingEntryStruct) error {
	return nil
}
func (fakeDatastoreSync) SyncACL(context.Context, uint64, dsdef.DatastoreACLEntryStruct) error {
	return nil
}
func (fakeDatastoreSync) SyncACLs(context.Context, uint64, []dsdef.DatastoreACLEntryStruct) error {
	return nil
}
func (fakeDatastoreSync) SyncGroupKeySet(context.Context, uint64, dsdef.DatastoreGroupKeySetStruct) error {
	return nil
}
func (fakeDatastoreSync) FetchEndpointList(context.Context, uint64) ([]dsdef.DatastoreEndpointEntryStruct, error) {
	return nil, nil
}
func (fakeDatastoreSync) FetchEndpointGroupList(context.Context, uint64, uint16) ([]dsdef.DatastoreGroupInformationEntryStruct, error) {
	return nil, nil
}
func (fakeDatastoreSync) FetchEndpointBindingList(context.Context, uint64, uint16) ([]dsdef.DatastoreEndpointBindingEntryStruct, error) {
	return nil, nil
}
func (fakeDatastoreSync) FetchGroupKeySetList(context.Context, uint64) ([]uint16, error) {
	return nil, nil
}
func (fakeDatastoreSync) FetchGroupKeySet(context.Context, uint64, uint16) (dsdef.DatastoreGroupKeySetStruct, error) {
	return dsdef.DatastoreGroupKeySetStruct{}, nil
}
func (fakeDatastoreSync) FetchACLList(context.Context, uint64) ([]dsdef.DatastoreACLEntryStruct, error) {
	return nil, nil
}

// fakeAdministratorPort is an administrator key without a CSR.
type fakeAdministratorPort struct{}

func (fakeAdministratorPort) IcacCsr(context.Context) ([]byte, error) { return nil, nil }

func jointFabricDevice() (*Device, error) {
	return New(&JointFabricAdministratorType, testOptions("jfa"),
		dsdef.Declare{Sync: fakeDatastoreSync{}}, jfadef.Declare{AdministratorPort: fakeAdministratorPort{}})
}

func jointFabricCases() []struct {
	cluster uint32
	build   func() (*Device, error)
	want    any
} {
	return []struct {
		cluster uint32
		build   func() (*Device, error)
		want    any
	}{
		{dsdef.ClusterID, jointFabricDevice, (*jointfabric.Datastore)(nil)},
		{jfadef.ClusterID, jointFabricDevice, (*jointfabric.Administrator)(nil)},
	}
}

// TestJointFabricKit: without the ports the generated servers stay; with
// them the kit's, the Administrator on the endpoint's Datastore.
func TestJointFabricKit(t *testing.T) {
	plain, err := New(&JointFabricAdministratorType, testOptions("jfa-plain"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.Server(dsdef.ClusterID).(*jointfabric.Datastore); ok {
		t.Fatal("the kit Datastore without a Sync")
	}
	if _, ok := plain.Server(jfadef.ClusterID).(*jointfabric.Administrator); ok {
		t.Fatal("the kit Administrator without an AdministratorPort")
	}
	d, err := jointFabricDevice()
	if err != nil {
		t.Fatal(err)
	}
	ds, _ := d.Server(dsdef.ClusterID).(*jointfabric.Datastore)
	if ds == nil {
		t.Fatal("no kit Datastore")
	}
	if err := ds.AddPendingNode(t.Context(), 1, "n"); err != nil {
		t.Fatal(err)
	}
	if got := dsdef.WrapOn(d.Server(dsdef.ClusterID), nil).Get().NodeList; len(got) != 1 {
		t.Fatalf("typed NodeList = %v", got)
	}
}
