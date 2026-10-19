// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// A port of matter.js packages/node/test/node/ScanNetworksRejectionCommissioningTest.ts
// at 862ad75f, the Thread case: a device that refuses ScanNetworks with a
// status (RejectingThreadServer, :125-143, StatusResponse.FailureError) does
// not end the Thread step — the flow adds the network after the refused
// scan. The Wi-Fi counterpart is TestNetworkWiFiScanRejectedThenAddFails;
// the device is the scripted netDevice of network_test.go, the dataset
// matter.js's MeshCoP dataset with only a NETWORK_NAME TLV ("MyNet",
// 03054d794e6574).

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
)

func TestScanNetworksRejectionParity_ThreadScanRejected(t *testing.T) {
	t.Parallel()
	dataset, err := hex.DecodeString("03054d794e6574")
	if err != nil {
		t.Fatal(err)
	}
	d := &netDevice{
		scan:      func(netcomm.ScanNetworksRequest) ([]byte, bool) { return nil, false },
		connected: true,
	}
	f := netFlow(t, d, netcomm.FeatureThreadNetworkInterface, func(o *Options) {
		o.ThreadNetwork = &ThreadNetwork{OperationalDataset: dataset}
	})
	if _, err := f.configureNetworkThread(context.Background()); err != nil {
		t.Fatalf("Thread step after a refused scan: %v", err)
	}
	// matter.js: networkCalls ["thread", "add"].
	calls := withoutArms(d.callsSoFar())
	if len(calls) < 2 || calls[0] != "scan" || calls[1] != "addThread" {
		t.Fatalf("calls %s, want scan then addThread", fmt.Sprint(calls))
	}
}
