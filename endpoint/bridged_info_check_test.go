// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	"github.com/SukramJ/go-fabric/contract"
)

// TestAssemblyRefusesBridgedInfoBeyondItsConstraint (R2-P6-wire-2502,
// decision 201): a raw Spec's stated BridgedDeviceBasicInformation value,
// or its VendorName, beyond the generated definition's constraint refuses
// the assembly, naming the attribute; a value within it — counted in
// UTF-16 code units, as matter.js's String.length — is served as stated,
// not cut at 32 bytes.
func TestAssemblyRefusesBridgedInfoBeyondItsConstraint(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 33)
	src := deviceTypeSource{dt: 0x0100, servers: []contract.ClusterServer{dtServer{id: 0x0006}}}
	assemble := func(s Spec) (*Topology, error) {
		a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1}, nil)
		if err != nil {
			t.Fatal(err)
		}
		s.StableKey, s.DeviceType, s.FriendlyName, s.Source = StringKey("lamp"), 0x0100, "Lamp", src
		return a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{s}}})
	}
	for name, s := range map[string]Spec{
		"ProductName": {BridgedBasicInformation: bdbidef.Patch{ProductName: &long}},
		"VendorName":  {VendorName: long},
	} {
		if _, err := assemble(s); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s of 33 code units: %v, want a refusal naming it", name, err)
		}
	}
	wide := strings.Repeat("ü", 32) // 64 bytes, 32 UTF-16 code units
	topo, err := assemble(Spec{VendorName: wide})
	if err != nil {
		t.Fatalf("VendorName of 32 code units: %v", err)
	}
	var lamp *Endpoint
	for _, ep := range topo.Endpoints {
		if ep.SourceKey != nil && ep.SourceKey.String() == "lamp" {
			lamp = ep
		}
	}
	for _, srv := range ClusterServers(lamp) {
		if srv.MatterClusterID() != core.BridgedDeviceBasicInformationClusterID {
			continue
		}
		if v, _ := srv.MatterRead(bdbidef.AttrVendorName); v != wide {
			t.Errorf("VendorName served %q, want the 64-byte value as stated", v)
		}
		return
	}
	t.Fatal("no BridgedDeviceBasicInformation server")
}

// TestBridgedInfoCheckIsTheServingEndpoints (R2-P6-wire-2502, round 3):
// the check is held only by a Spec that serves BridgedDeviceBasicInformation.
// A part — which serves no such cluster — assembles whatever its Spec
// states. The bridge's own VendorName is checked once, by Config.Validate
// (TestConfigValidate_VendorNameBeyondItsConstraint, decision 201).
func TestBridgedInfoCheckIsTheServingEndpoints(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 40)
	src := func() contract.EndpointSource {
		return deviceTypeSource{dt: 0x0100, servers: []contract.ClusterServer{dtServer{id: 0x0006}}}
	}
	a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1, VendorName: "Bridge"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{{
		StableKey: StringKey("owner"), DeviceType: 0x0100, FriendlyName: "Owner", Source: src(),
		Parts: []Spec{{
			StableKey: StringKey("part"), DeviceType: 0x0100, FriendlyName: "Part", Source: src(),
			VendorName: long, BridgedBasicInformation: bdbidef.Patch{ProductName: &long},
		}},
	}}}})
	if err != nil {
		t.Fatalf("a part's statements beyond the constraint: %v, want the assembly", err)
	}
}
