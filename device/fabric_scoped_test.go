// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
)

// TestFabricScopedDefaults: Binding and EcosystemInformation, whose
// generated server refuses a fabric-scoped attribute, default to the core
// servers (matter.js BindingServer where Base mandates Binding,
// EcosystemInformationServer); OtaSoftwareUpdateRequestor stays the host's.
func TestFabricScopedDefaults(t *testing.T) {
	t.Parallel()
	bell, err := NewDoorbell(testOptions("bell"))
	if err != nil {
		t.Fatal(err)
	}
	if bell.Binding() == nil {
		t.Error("Doorbell mounts no default Binding")
	}
	if v, ok := bell.Binding().MatterRead(0x0000); !ok || v == nil {
		t.Errorf("Binding list %v, %v", v, ok)
	}

	node, err := NewBridgedNode(testOptions("node"), WithOptional(ecodef.ClusterID))
	if err != nil {
		t.Fatal(err)
	}
	eco := node.EcosystemInformation()
	if eco == nil {
		t.Fatal("BridgedNode mounts no EcosystemInformation when opted in")
	}
	for _, attr := range []uint32{ecodef.AttrDeviceDirectory, ecodef.AttrLocationDirectory} {
		v, ok := eco.MatterRead(attr)
		list, isList := v.(spec.List[spec.Encodable])
		if !ok || !isList || len(list) != 0 {
			t.Errorf("attribute 0x%04X = %v (%T), want an empty list", attr, v, v)
		}
	}

	if _, err := NewDoorbell(testOptions("b2"), WithInitial(0x001E, map[uint32]any{0: nil})); !errors.Is(err, ErrConfiguration) {
		t.Errorf("initial values for Binding: %v, want ErrConfiguration", err)
	}
	if _, err := NewOtaRequestor(testOptions("ota")); !errors.Is(err, ErrNoDefaultServer) {
		t.Errorf("OtaRequestor without its requestor: %v, want ErrNoDefaultServer", err)
	}
}
