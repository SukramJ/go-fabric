// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	acdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	gcdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	gddef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	lcdef "github.com/SukramJ/go-fabric/cluster/spec/localizationconfiguration"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	uldef "github.com/SukramJ/go-fabric/cluster/spec/unitlocalization"
	"github.com/SukramJ/go-fabric/contract"
)

// TestHostServerMountsAListOfServers (#220): a host whose servers are a
// list — ten of them, three without a generated package — declares each
// with HostServer alone, and the device mounts each as the server of its
// cluster.
func TestHostServerMountsAListOfServers(t *testing.T) {
	t.Parallel()

	ids := []uint32{
		bidef.ClusterID, gcdef.ClusterID, gddef.ClusterID, acdef.ClusterID,
		lcdef.ClusterID, tfldef.ClusterID, uldef.ClusterID,
		accessControlID, operationalCredentialsID, groupKeyManagementID,
	}
	servers := make([]contract.ClusterServer, 0, len(ids))
	decls := make([]Declaration, 0, len(ids))
	for _, id := range ids {
		srv := &hostServer{id: id}
		servers = append(servers, srv)
		decls = append(decls, HostServer(srv))
	}
	root, err := NewRootNode(testOptions("root"), decls...)
	if err != nil {
		t.Fatalf("NewRootNode over HostServer: %v", err)
	}
	for _, srv := range servers {
		if got := root.Server(srv.MatterClusterID()); got != srv {
			t.Errorf("cluster 0x%04X: mounted %v, want the host's server", srv.MatterClusterID(), got)
		}
	}
	if u := root.BasicInformation().Underlying(); u != servers[0] {
		t.Errorf("BasicInformation accessor wraps %v, want the host's server", u)
	}
}

// TestHostServerResolvesTheGeneratedDeclaration: for a cluster with a
// generated package HostServer is that package's Declare with the server
// as HostServer — the same declaration a host writes by hand, refused
// twice like it; for a cluster without one it is WithServer's mount.
func TestHostServerResolvesTheGeneratedDeclaration(t *testing.T) {
	t.Parallel()

	srv := &hostServer{id: onoffdef.ClusterID}
	d, ok := HostServer(srv).(onoffdef.Declare)
	if !ok || d.HostServer != srv {
		t.Fatalf("HostServer(OnOff server) = %#v, want onoffdef.Declare{HostServer: srv}", HostServer(srv))
	}
	opts := testOptions("lamp")
	if _, err := NewOnOffLight(opts, HostServer(srv), onoffdef.Declare{HostServer: srv}); !errors.Is(err, ErrConfiguration) || !strings.Contains(err.Error(), "declared twice") {
		t.Errorf("HostServer beside the typed Declare: %v, want declared twice", err)
	}

	vendor := &hostServer{id: 0xFFF1_FC00}
	o, ok := HostServer(vendor).(clusterOption)
	if !ok || o.decl.Server != vendor || !o.undeclared {
		t.Errorf("HostServer(manufacturer-specific) = %#v, want WithServer's mount", HostServer(vendor))
	}
	if _, err := NewOnOffLight(opts, HostServer(vendor)); err != nil {
		t.Errorf("a manufacturer-specific server over HostServer: %v", err)
	}
	if _, err := NewOnOffLight(opts, HostServer(nil)); !errors.Is(err, ErrConfiguration) {
		t.Errorf("HostServer(nil): %v, want ErrConfiguration", err)
	}
}

// TestHostServerTableCoversTheRegistry: every entry of the generated
// table is a registered definition's cluster and declares that cluster
// with the host's server.
func TestHostServerTableCoversTheRegistry(t *testing.T) {
	t.Parallel()

	for id, declare := range hostServers {
		if spec.Lookup(id) == nil {
			t.Errorf("0x%04X: no registered definition", id)
		}
		srv := &hostServer{id: id}
		d := declare(srv)
		if d.ClusterID() != id || d.ClusterDeclaration().Server != srv {
			t.Errorf("0x%04X: declares 0x%04X over %v", id, d.ClusterID(), d.ClusterDeclaration().Server)
		}
	}
}
