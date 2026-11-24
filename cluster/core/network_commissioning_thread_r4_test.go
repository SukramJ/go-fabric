// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
)

// TestThreadExpiryWithNoNetworkLeftClearsLast (R4-par-4002): the fail-safe's
// expiry clears LastNetworkingStatus, LastNetworkID and LastConnectErrorValue
// whenever no network is left after the revert — not only when the window
// took an empty backup. A border router without a dataset whose window saw
// only a ScanNetworks (which sets LastNetworkingStatus and takes no backup)
// kept the scan's status before. The three are reported when one of them
// held a value; an expiry that finds them null reports none of them.
func TestThreadExpiryWithNoNetworkLeftClearsLast(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	last := []uint32{netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue}

	t.Run("scan only", func(t *testing.T) {
		t.Parallel()
		br := &simBorderRouter{}
		armed, crumbs := true, []uint64{}
		n := newThreadNC(t, br, &armed, &crumbs)
		heard := threadChangeListener(t, n)
		if _, err := n.MatterInvoke(ctx, netcommdef.CmdScanNetworks, netcommdef.ScanNetworksRequest{}); err != nil {
			t.Fatal(err)
		}
		if st, _ := n.MatterRead(netcommAttrLastNetworkingStatus); st == nil {
			t.Fatal("the scan set no LastNetworkingStatus: the case does not hold its premise")
		}
		heard.take()
		n.OnFailSafeExpired(ctx)
		for _, a := range last {
			if v, _ := n.MatterRead(a); v != nil {
				t.Errorf("attribute 0x%04X = %v after the expiry with no network left, want null", a, v)
			}
		}
		if got := heard.take(); !hasAll(got, last...) {
			t.Errorf("the expiry reported %v, want the three Last… attributes", got)
		}
	})

	t.Run("nothing held", func(t *testing.T) {
		t.Parallel()
		br := &simBorderRouter{}
		armed, crumbs := true, []uint64{}
		n := newThreadNC(t, br, &armed, &crumbs)
		heard := threadChangeListener(t, n)
		n.OnFailSafeExpired(ctx)
		if got := heard.take(); slices.ContainsFunc(got, func(a uint32) bool { return slices.Contains(last, a) }) {
			t.Errorf("an expiry over three null Last… attributes reported %v", got)
		}
	})
}
