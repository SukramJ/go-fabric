// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"testing"

	actionsdef "github.com/SukramJ/go-fabric/cluster/spec/actions"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	"github.com/SukramJ/go-fabric/node"
)

type routeActions struct{}

func (routeActions) ActionList() []actionsdef.ActionStruct                  { return nil }
func (routeActions) EndpointLists() []actionsdef.EndpointListStruct         { return nil }
func (routeActions) HandleAction(context.Context, node.ActionRequest) error { return nil }

type routeEcosystem struct{}

func (routeEcosystem) EcosystemDevices(context.Context) []ecodef.EcosystemDeviceStruct {
	return nil
}

func (routeEcosystem) EcosystemLocations(context.Context) []ecodef.EcosystemLocationStruct {
	return nil
}

// TestNodeFeatureRoutes: the node-feature routes are the lower packages'
// own — the Actions and EcosystemInformation servers build, an OTA image
// written by CreateOTAImage reads back through ReadOTAImage, a ledger
// client names its endpoint.
func TestNodeFeatureRoutes(t *testing.T) {
	t.Parallel()
	actions, err := node.NewActions(node.ActionsConfig{Host: routeActions{}})
	if err != nil || actions.MatterClusterID() != actionsdef.ClusterID {
		t.Fatalf("NewActions: %v", err)
	}
	if _, err := node.NewActions(node.ActionsConfig{}); err == nil {
		t.Error("NewActions without a host built")
	}
	if eco := node.NewEcosystemInformation(routeEcosystem{}); eco.MatterClusterID() != ecodef.ClusterID {
		t.Errorf("NewEcosystemInformation: cluster 0x%04X", eco.MatterClusterID())
	}
	payload := []byte("firmware")
	img, err := node.CreateOTAImage(node.OTAImageOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2, SoftwareVersionString: "2.0", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	h, err := node.ReadOTAImage(bytes.NewReader(img), &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.SoftwareVersion != 2 || !bytes.Equal(out.Bytes(), payload) {
		t.Errorf("header %+v, payload %q", h, out.Bytes())
	}
	if c := node.NewDCLClient(node.DCLTestNetURL); c.BaseURL != node.DCLTestNetURL {
		t.Errorf("DCL client at %q", c.BaseURL)
	}
	if node.OTAConsentGranted == node.OTAConsentDenied {
		t.Error("the consent states collapse")
	}
}
