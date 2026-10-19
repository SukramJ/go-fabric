// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
)

// TestIncreaseConfigurationVersionRaisesTheDeviceAndTheNode pins matter.js
// BridgedDeviceBasicInformationServer.increaseConfigurationVersion: every
// bridged endpoint of the device gets the next version, and so — once — does
// the node's BasicInformation, because a bridged node's configuration change
// is the bridge's. TC-BRBINFO-3.2 drives it through the CHIP app pipe
// (SimulateConfigurationVersionChange).
func TestIncreaseConfigurationVersionRaisesTheDeviceAndTheNode(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	bi, err := mattercore.NewBasicInformation(mattercore.Config{VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "n", VendorName: "v", ProductName: "p", SerialNumber: "s"})
	if err != nil {
		t.Fatal(err)
	}
	b.AttachRootClusters([]contract.ClusterServer{bi})

	got := b.IncreaseConfigurationVersion("ccu1", "MANYTMP")
	if len(got.Bridged) != 30 {
		t.Fatalf("raised %d bridged endpoints, want the device's 30", len(got.Bridged))
	}
	for ep, v := range got.Bridged {
		if v != 2 {
			t.Errorf("endpoint %d: version %d, want 2", ep, v)
		}
		if served := b.Topology().FindByID(ep).ConfigurationVersion(); served != 2 {
			t.Errorf("endpoint %d serves %d, want 2", ep, served)
		}
	}
	if got.Node != 2 {
		t.Errorf("node version %d, want 2 (raised once for the device)", got.Node)
	}
	if v, _ := bi.MatterRead(0x0018); v != uint32(2) {
		t.Errorf("BasicInformation.ConfigurationVersion = %v, want 2", v)
	}
	if none := b.IncreaseConfigurationVersion("ccu1", "NO-SUCH-DEVICE"); len(none.Bridged) != 0 || none.Node != 0 {
		t.Errorf("an unknown device raised %+v", none)
	}
}

// TestIncreaseNodeConfigurationVersionWithRaisesTheNodeOnce pins the
// grouped form (matter.js BasicInformationServer.increaseConfigurationVersion
// with the bridged nodes' changes under its context): the device raised
// inside the change gets its next version, the node one increment however
// many devices changed, and a nil change raises the node alone.
func TestIncreaseNodeConfigurationVersionWithRaisesTheNodeOnce(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	bi, err := mattercore.NewBasicInformation(mattercore.Config{VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "n", VendorName: "v", ProductName: "p", SerialNumber: "s"})
	if err != nil {
		t.Fatal(err)
	}
	b.AttachRootClusters([]contract.ClusterServer{bi})

	got := b.IncreaseNodeConfigurationVersionWith(func(c *ConfigurationChange) {
		if raised := c.IncreaseConfigurationVersion("ccu1", "MANYTMP"); len(raised) != 30 {
			t.Errorf("raised %d endpoints, want 30", len(raised))
		}
		if raised := c.IncreaseConfigurationVersion("ccu1", "NO-SUCH-DEVICE"); len(raised) != 0 {
			t.Errorf("an unknown device raised %v", raised)
		}
	})
	if got.Node != 2 || len(got.Bridged) != 30 {
		t.Errorf("grouped change = node %d, %d bridged; want 2, 30", got.Node, len(got.Bridged))
	}
	if alone := b.IncreaseNodeConfigurationVersionWith(nil); alone.Node != 3 || len(alone.Bridged) != 0 {
		t.Errorf("nil change = %+v, want node 3 alone", alone)
	}
	var nilBridge *Bridge
	if none := nilBridge.IncreaseNodeConfigurationVersionWith(nil); none.Node != 0 {
		t.Errorf("nil bridge raised %+v", none)
	}
}
