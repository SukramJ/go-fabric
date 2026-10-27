// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/netinfra"
	tdirdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	"github.com/SukramJ/go-fabric/device"
)

// netinfraDataset is chip's kDataset1 (TestThreadNetworkDirectoryCluster.cpp:
// 49-60 at 6170af84): Extended PAN ID 39758ec8144b07fb, Active Timestamp 1.
var netinfraDataset = []byte{
	0x0e, 0x08, 0, 0, 0, 0, 0, 0, 0, 0x01, 0x00, 0x03, 0x00, 0x00, 0x0f, 0x35, 0x04, 0x07, 0xff, 0xf8, 0x00,
	0x02, 0x08, 0x39, 0x75, 0x8e, 0xc8, 0x14, 0x4b, 0x07, 0xfb,
	0x07, 0x08, 0xfd, 0xf1, 0xf1, 0xad, 0xd0, 0x79, 0x7d, 0xc0,
	0x05, 0x10, 0xf3, 0x66, 0xce, 0xc7, 0xa4, 0x46, 0xba, 0xb9, 0x78, 0xd9, 0x0d, 0x27, 0xab, 0xe3, 0x8f, 0x23,
	0x03, 0x0f, 0x4f, 0x70, 0x65, 0x6e, 0x54, 0x68, 0x72, 0x65, 0x61, 0x64, 0x2d, 0x35, 0x39, 0x33, 0x38,
	0x01, 0x02, 0x59, 0x38,
	0x04, 0x10, 0x3c, 0xa6, 0x7c, 0x96, 0x9e, 0xfb, 0x0d, 0x0c, 0x74, 0xa4, 0xd8, 0xee, 0x92, 0x3b, 0x57, 0x6c,
	0x0c, 0x04, 0x02, 0xa0, 0xf7, 0xf8,
}

// TestNetworkInfrastructureManagerDeclaresTheNetinfraServers: the kit
// mounts cluster/netinfra's servers on a NetworkInfrastructureManager; a
// declared ThreadNetworkStore is the directory's, its networks served, and
// the Initial's PreferredExtendedPanId is the preferred one. The host sets
// the Wi-Fi credentials on the mounted server.
func TestNetworkInfrastructureManagerDeclaresTheNetinfraServers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := netinfra.NewMemoryThreadNetworkStore(2)
	extPanID := netinfraDataset[23:31]
	if err := store.SaveNetwork(ctx, extPanID, netinfraDataset); err != nil {
		t.Fatal(err)
	}
	d, err := device.NewNetworkInfrastructureManager(device.Options{StableKey: device.StringKey("nim"), Name: "nim"},
		tdirdef.Declare{ThreadNetworkStore: store, Initial: tdirdef.Patch{PreferredExtendedPanId: &extPanID}})
	if err != nil {
		t.Fatal(err)
	}
	st := d.ThreadNetworkDirectory().Get()
	if st.ThreadNetworkTableSize != 2 || len(st.ThreadNetworks) != 1 || st.ThreadNetworks[0].NetworkName != "OpenThread-5938" {
		t.Errorf("directory state %+v", st)
	}
	if st.PreferredExtendedPanId == nil || !bytes.Equal(*st.PreferredExtendedPanId, extPanID) {
		t.Errorf("PreferredExtendedPanId %v, want %X", st.PreferredExtendedPanId, extPanID)
	}

	wifi := d.WiFiNetworkManagement().Host()
	if wifi == nil {
		t.Fatalf("WiFiNetworkManagement is %T, no Host", d.WiFiNetworkManagement().Underlying())
	}
	if err := wifi.SetNetworkCredentials([]byte("home"), []byte("password123")); err != nil {
		t.Fatal(err)
	}
	if ssid := d.WiFiNetworkManagement().Get().Ssid; ssid == nil || string(*ssid) != "home" {
		t.Errorf("Ssid %v, want home", ssid)
	}

	// A preferred network the store does not hold refuses the device.
	other := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if _, err := device.NewNetworkInfrastructureManager(device.Options{StableKey: device.StringKey("nim2"), Name: "nim2"},
		tdirdef.Declare{Initial: tdirdef.Patch{PreferredExtendedPanId: &other}}); err == nil {
		t.Error("a preferred network outside the store was accepted")
	}
}
