// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"testing"
	"time"

	descriptordef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	pscdef "github.com/SukramJ/go-fabric/cluster/spec/powersourceconfiguration"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestBridgedPowerSourceConfiguration: an OnOffLight that declares
// PowerSource states BridgedPowerSourceInfo, under which BridgedNode makes
// PowerSourceConfiguration mandatory ("BridgedPowerSourceInfo, D": a
// trailing D announces a deprecation, the terms before it decide —
// matter.js Conformance.ts:936-940 at 862ad75f); the kit mounts it, so the
// node reports no device-type violation, and a controller reads its
// Sources.
func TestBridgedPowerSourceConfiguration(t *testing.T) {
	t.Parallel()
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "light"},
		psdef.Declare{Features: psdef.FeatureBattery, Initial: psdef.Patch{
			Order: new(uint8(0)), BatReplacementNeeded: new(false),
		}})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	if v := pair.Device.Bridge().DeviceTypeViolations(); len(v) != 0 {
		t.Fatalf("device-type violations: %+v", v)
	}
	ep, _ := light.EndpointNumber()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	desc, err := fabrictest.ReadCluster(ctx, pair, ep, descriptordef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	ds, _ := descriptordef.StateFrom(desc)
	found := false
	for _, id := range ds.ServerList {
		found = found || id == pscdef.ClusterID
	}
	if !found {
		t.Fatalf("ServerList %v lacks PowerSourceConfiguration", ds.ServerList)
	}
	values, err := fabrictest.ReadCluster(ctx, pair, ep, pscdef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	if _, absent := pscdef.StateFrom(values); len(absent) != 0 {
		t.Fatalf("PowerSourceConfiguration attributes not served: %v", absent)
	}
}
