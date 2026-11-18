// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/jointfabric"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/contract"
)

// The kit's Joint Fabric servers (cluster/jointfabric, brief 26-D): the
// JointFabricAdministrator device type (0x0130, matter.js's
// endpoints/joint-fabric-administrator.ts) mandates both clusters, and
// each is the module's server once its declaration carries its port —
// the Datastore its Sync (and, optionally, its Store under the device's
// StableKey), the Administrator its AdministratorPort (and, optionally,
// its CommissionerReach and TrustAnchors); without one the generated
// server stays. The
// Administrator takes the endpoint's kit Datastore as the anchor's.

func init() {
	selfHeldDefaults[dsdef.ClusterID] = newJointFabricDatastoreServer
	selfHeldDefaults[jfadef.ClusterID] = newJointFabricAdministratorServer
}

// newJointFabricDatastoreServer is the module's JointFabricDatastore
// server ([jointfabric.NewDatastore]) on the declared Sync and Store.
func newJointFabricDatastoreServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(dsdef.Declare)
	if d.Sync == nil {
		return nil, notServed(fmt.Errorf("joint fabric datastore: %w", errNoPort))
	}
	return jointfabric.NewDatastore(context.Background(), jointfabric.DatastoreConfig{
		Sync: d.Sync, Store: d.Store, Key: b.key,
	})
}

// newJointFabricAdministratorServer is the module's
// JointFabricAdministrator server ([jointfabric.NewAdministrator]) on the
// declared AdministratorPort, CommissionerReach and TrustAnchors; Initial's
// AdministratorFabricIndex is set on it. Without TrustAnchors the Vendor
// ID Verification fails closed (owner decisions 190 and 235).
func newJointFabricAdministratorServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(jfadef.Declare)
	if d.AdministratorPort == nil {
		return nil, notServed(fmt.Errorf("joint fabric administrator: %w", errNoPort))
	}
	ds, _ := b.sibling(dsdef.ClusterID).(*jointfabric.Datastore)
	cfg := jointfabric.AdministratorConfig{
		Port: d.AdministratorPort, Reach: d.CommissionerReach, Datastore: ds,
	}
	if d.TrustAnchors != nil {
		cfg.TrustAnchors = d.TrustAnchors.LookupTrustAnchor
	}
	return jointfabric.NewAdministrator(cfg)
}
