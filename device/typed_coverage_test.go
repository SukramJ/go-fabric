// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"testing"

	acdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	gcdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	gddef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	otardef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/contract"
)

// The root clusters no generated
// definition is committed for.
const (
	accessControlID          = 0x001F
	operationalCredentialsID = 0x003E
	groupKeyManagementID     = 0x003F
)

// hostServerOf is a host's own server of a cluster, with nothing but the
// contract.
func hostServerOf(id uint32) contract.ClusterServer { return bareServer{id: id} }

// TestRootNodeAccessorsWrapTheMountedServers: a RootNode whose mandatory
// servers are the host's has a typed accessor for each cluster of the
// type, the mandatory ones over the mounted server and the optional ones
// over none.
func TestRootNodeAccessorsWrapTheMountedServers(t *testing.T) {
	t.Parallel()

	root, err := NewRootNode(testOptions("root"),
		bidef.Declare{HostServer: hostServerOf(bidef.ClusterID)},
		gcdef.Declare{HostServer: hostServerOf(gcdef.ClusterID)},
		gddef.Declare{HostServer: hostServerOf(gddef.ClusterID)},
		acdef.Declare{HostServer: hostServerOf(acdef.ClusterID)},
		WithServer(hostServerOf(accessControlID)),
		WithServer(hostServerOf(operationalCredentialsID)),
		WithServer(hostServerOf(groupKeyManagementID)),
	)
	if err != nil {
		t.Fatalf("NewRootNode: %v", err)
	}
	if root.Type() != &RootNodeType {
		t.Errorf("type %v", root.Type().Name)
	}
	for name, got := range map[string]struct {
		underlying contract.ClusterServer
		id         uint32
	}{
		"BasicInformation":           {root.BasicInformation().Underlying(), bidef.ClusterID},
		"GeneralCommissioning":       {root.GeneralCommissioning().Underlying(), gcdef.ClusterID},
		"GeneralDiagnostics":         {root.GeneralDiagnostics().Underlying(), gddef.ClusterID},
		"AdministratorCommissioning": {root.AdministratorCommissioning().Underlying(), acdef.ClusterID},
	} {
		if got.underlying == nil || got.underlying.MatterClusterID() != got.id {
			t.Errorf("%s: wraps %v, want the mounted server of 0x%04X", name, got.underlying, got.id)
		}
	}
	for name, absent := range map[string]bool{
		"LocalizationConfiguration":  root.LocalizationConfiguration() == nil,
		"TimeFormatLocalization":     root.TimeFormatLocalization() == nil,
		"UnitLocalization":           root.UnitLocalization() == nil,
		"PowerSourceConfiguration":   root.PowerSourceConfiguration() == nil,
		"NetworkCommissioning":       root.NetworkCommissioning() == nil,
		"DiagnosticLogs":             root.DiagnosticLogs() == nil,
		"SoftwareDiagnostics":        root.SoftwareDiagnostics() == nil,
		"WiFiNetworkDiagnostics":     root.WiFiNetworkDiagnostics() == nil,
		"EthernetNetworkDiagnostics": root.EthernetNetworkDiagnostics() == nil,
		"TimeSynchronization":        root.TimeSynchronization() == nil,
		"IcdManagement":              root.IcdManagement() == nil,
		"Groupcast":                  root.Groupcast() == nil,
	} {
		if !absent {
			t.Errorf("%s: wraps a server, want nil until a declaration mounts one", name)
		}
	}
}

// TestOtaRequestorAccessorWrapsTheHostsServer: the requestor is the
// host's, and the typed accessor wraps it.
func TestOtaRequestorAccessorWrapsTheHostsServer(t *testing.T) {
	t.Parallel()

	ota, err := NewOtaRequestor(testOptions("ota"), otardef.Declare{HostServer: hostServerOf(otardef.ClusterID)})
	if err != nil {
		t.Fatalf("NewOtaRequestor: %v", err)
	}
	if got := ota.OtaSoftwareUpdateRequestor().Underlying(); got == nil || got.MatterClusterID() != otardef.ClusterID {
		t.Errorf("OtaSoftwareUpdateRequestor wraps %v", got)
	}
}
