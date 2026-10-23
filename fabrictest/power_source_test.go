// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"slices"
	"testing"
	"time"

	powersourcedef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestPowerSourceEndpointList: a declared PowerSource names its own
// endpoint in EndpointList unless the declaration's Initial names others.
func TestPowerSourceEndpointList(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	own, err := device.NewPowerSource(device.Options{StableKey: device.StringKey("own"), Name: "own"},
		powersourcedef.Declare{Features: powersourcedef.FeatureWired, Initial: powersourcedef.Patch{Order: new(uint8(0))}})
	if err != nil {
		t.Fatal(err)
	}
	named := []uint16{7, 8}
	declared, err := device.NewPowerSource(device.Options{StableKey: device.StringKey("declared"), Name: "declared"},
		powersourcedef.Declare{Features: powersourcedef.FeatureWired, Initial: powersourcedef.Patch{Order: new(uint8(1)), EndpointList: &named}})
	if err != nil {
		t.Fatal(err)
	}
	if got := own.PowerSource().Get().EndpointList; len(got) != 0 {
		t.Errorf("before assembly: EndpointList %v, want empty", got)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{own, declared}}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	read := func(d *device.PowerSource) powersourcedef.State {
		t.Helper()
		ep, _ := d.EndpointNumber()
		v, err := fabrictest.ReadCluster(ctx, pair, ep, powersourcedef.ClusterID)
		if err != nil {
			t.Fatal(err)
		}
		st, _ := powersourcedef.StateFrom(v)
		return st
	}
	ownEP, _ := own.EndpointNumber()
	st := read(own)
	if got := st.EndpointList; !slices.Equal(got, []uint16{ownEP}) {
		t.Errorf("EndpointList %v, want the device's own endpoint %d", got, ownEP)
	}
	// matter.js PowerSourceServer.initialize's defaults for a wired source.
	if st.Status != powersourcedef.PowerSourceStatusUnspecified || st.Description != "Mains power" || st.WiredCurrentType != powersourcedef.WiredCurrentTypeAc {
		t.Errorf("defaults: status %v, description %q, current type %v", st.Status, st.Description, st.WiredCurrentType)
	}
	if got := read(declared).EndpointList; !slices.Equal(got, named) {
		t.Errorf("declared EndpointList %v, want %v", got, named)
	}
}
