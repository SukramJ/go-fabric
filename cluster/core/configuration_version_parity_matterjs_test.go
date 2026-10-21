// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/basic-information/ConfigurationVersionTest.ts
// at 862ad75f: the root's ConfigurationVersion raised after a change, never
// lowered, and raised once for a change grouping several bridged nodes.
//
// matter.js runs the node as a MockServerNode (createBridge with bridged
// OnOffLights under an Aggregator) and changes it in node.act; the port runs
// a fabrictest device with two bridged OnOffLights under the node's
// Aggregator, changes it through its bridge (bridge.Bridge
// IncreaseNodeConfigurationVersionWith, the Go form of
// increaseConfigurationVersion(change) and of the shared context) and reads
// the versions locally, as stateOf does: the root BasicInformation server's
// ConfigurationVersion attribute and each bridged endpoint's
// ConfigurationVersion. "bumps the bridge for a standalone bridged-node
// change" is bridge:TestIncreaseConfigurationVersionRaisesTheDeviceAndTheNode.
package core_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// configVersionDevice is a fabrictest device serving bridged OnOffLights
// with the given keys (each its own physical device, DeviceAddress = key).
func configVersionDevice(t *testing.T, keys ...string) *fabrictest.Device {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	eps := make([]node.Endpoint, 0, len(keys))
	for _, k := range keys {
		l, err := device.NewOnOffLight(device.Options{
			StableKey: endpoint.StringKey(k), Name: k, DeviceAddress: k,
		})
		if err != nil {
			t.Fatal(err)
		}
		eps = append(eps, l)
	}
	return site.AddDevice(fabrictest.Topology{Bridged: eps})
}

// rootBasicInformation is the device's root BasicInformation server.
func rootBasicInformation(t *testing.T, d *fabrictest.Device) *core.BasicInformation {
	t.Helper()
	root := d.Bridge().Topology().FindByID(0)
	if root == nil {
		t.Fatal("no root endpoint")
	}
	for _, srv := range endpoint.ClusterServers(root) {
		if bi, ok := srv.(*core.BasicInformation); ok {
			return bi
		}
	}
	t.Fatal("no BasicInformation server on the root endpoint")
	return nil
}

// configVersion is node.stateOf(BasicInformationServer).configurationVersion.
func configVersion(t *testing.T, d *fabrictest.Device) uint32 {
	t.Helper()
	v, ok := rootBasicInformation(t, d).MatterRead(0x0018) // ConfigurationVersion
	u, isU32 := v.(uint32)
	if !ok || !isU32 {
		t.Fatalf("ConfigurationVersion = %v (%T), %v", v, v, ok)
	}
	return u
}

// bridgedConfigVersion is the bridged endpoint's
// BridgedDeviceBasicInformation ConfigurationVersion.
func bridgedConfigVersion(t *testing.T, d *fabrictest.Device, key string) uint32 {
	t.Helper()
	id, ok := d.EndpointOf(key)
	if !ok {
		t.Fatalf("no endpoint %q", key)
	}
	return d.Bridge().Topology().FindByID(id).ConfigurationVersion()
}

// TestConfigurationVersionRunsTheChangeThenIncrements ports "runs the change
// callback and increments afterwards" (ConfigurationVersionTest.ts:24-37).
// The callback also sees the version not yet raised: the increment is
// afterwards (BasicInformationServer.ts:132-138).
func TestConfigurationVersionRunsTheChangeThenIncrements(t *testing.T) {
	t.Parallel()
	d := configVersionDevice(t)
	before := configVersion(t, d)

	ran := false
	d.Bridge().IncreaseNodeConfigurationVersionWith(func(*bridge.ConfigurationChange) {
		ran = true
		if got := configVersion(t, d); got != before {
			t.Errorf("version inside the change = %d, want %d (raised afterwards)", got, before)
		}
	})

	if !ran {
		t.Error("the change callback did not run")
	}
	if got := configVersion(t, d); got != before+1 {
		t.Errorf("ConfigurationVersion = %d, want %d", got, before+1)
	}
}

// TestConfigurationVersionRefusesADecrease ports "rejects a decrease of
// configurationVersion" (ConfigurationVersionTest.ts:39-53). matter.js
// throws "must not decrease" from the $Changing reactor
// (BasicInformationServer.ts:148-153) when the state is set to 1 after an
// increase. The Go server has no setter that could lower the version: the
// one way to install a value, RestoreConfigurationVersion, ignores a lower
// one, so the Go assertion is the attribute value, still 2.
func TestConfigurationVersionRefusesADecrease(t *testing.T) {
	t.Parallel()
	d := configVersionDevice(t)
	d.Bridge().IncreaseNodeConfigurationVersionWith(nil)
	raised := configVersion(t, d)
	if raised != 2 {
		t.Fatalf("after one increase ConfigurationVersion = %d, want 2", raised)
	}

	rootBasicInformation(t, d).RestoreConfigurationVersion(1)

	if got := configVersion(t, d); got != raised {
		t.Errorf("ConfigurationVersion = %d after restoring 1, want %d (never lowered)", got, raised)
	}
}

// TestConfigurationVersionGroupsBridgedChanges ports "bumps the bridge once
// when grouping bridged-node changes via a shared context"
// (ConfigurationVersionTest.ts:55-89): two bridged-node increases inside
// the root's change raise the root once and each bridged node to 2
// (BridgedDeviceBasicInformationServer.ts:72-90).
func TestConfigurationVersionGroupsBridgedChanges(t *testing.T) {
	t.Parallel()
	d := configVersionDevice(t, "lightA", "lightB")
	before := configVersion(t, d)

	d.Bridge().IncreaseNodeConfigurationVersionWith(func(c *bridge.ConfigurationChange) {
		c.IncreaseConfigurationVersion(node.Scope, "lightA")
		c.IncreaseConfigurationVersion(node.Scope, "lightB")
	})

	if got := configVersion(t, d); got != before+1 {
		t.Errorf("bridge ConfigurationVersion = %d, want %d (one increment)", got, before+1)
	}
	for _, k := range []string{"lightA", "lightB"} {
		if got := bridgedConfigVersion(t, d, k); got != 2 {
			t.Errorf("%s ConfigurationVersion = %d, want 2", k, got)
		}
	}
}
