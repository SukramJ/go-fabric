// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The device type of the commissionable advertisement (the DT key and the
// _T<id> subtype) when the host states none: inferred from the endpoints
// the node serves, as matter.js's ProductDescriptionServer infers it at
// db021da0 (packages/node/src/behavior/system/product-description/
// ProductDescriptionServer.ts:43-63, :93-126) and
// CommissionableMdnsAdvertisement.ts sends it (:48, :87). matter.js has no
// test of the inference; these cases state the lines they hold.
package node_test

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/onoff"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/node"
)

// typedDevice is a stub bridged device of a given device type holding an
// onoff.Server.
type typedDevice struct {
	key        string
	deviceType uint16
	srv        *onoff.Server
}

func newTypedDevice(t *testing.T, key string, deviceType uint16) *typedDevice {
	t.Helper()
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	return &typedDevice{key: key, deviceType: deviceType, srv: srv}
}

func (d *typedDevice) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey(d.key), DeviceAddress: d.key, ChannelAddress: d.key + ":0",
		DeviceType: d.deviceType, FriendlyName: d.key, Source: d,
	}
}

func (d *typedDevice) MatterDeviceType() uint16 { return d.deviceType }

func (d *typedDevice) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{d.srv}
}

// advertisedDeviceType returns the DT value of the published commissionable
// record and whether its subtypes carry the matching _T<id>.
func advertisedDeviceType(t *testing.T, adv *mdns.Noop) uint64 {
	t.Helper()
	services := adv.Active()
	for i := range services {
		svc := &services[i]
		if svc.ServiceType != mdns.ServiceTypeCommissionable {
			continue
		}
		var dt uint64
		found := false
		for _, txt := range svc.TXT {
			if txt.Key == "DT" {
				dt, _ = strconv.ParseUint(txt.Value, 10, 32)
				found = true
			}
		}
		if !found {
			t.Fatal("the commissionable record carries no DT")
		}
		if sub := "_T" + strconv.FormatUint(dt, 10); !slices.Contains(svc.Subtypes, sub) {
			t.Errorf("subtypes %v lack %s", svc.Subtypes, sub)
		}
		return dt
	}
	t.Fatal("no commissionable record is published")
	return 0
}

// startAdvertising starts a node on cfg with the given devices on its
// Aggregator and returns it and its advertiser.
func startAdvertising(t *testing.T, cfg node.Config, devices ...node.Endpoint) (*node.Node, *mdns.Noop) {
	t.Helper()
	adv := mdns.NewNoop()
	cfg.Advertiser = adv
	n := newNode(t, cfg)
	for _, d := range devices {
		if err := n.Aggregator().Add(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return n, adv
}

func TestParityMatterJS_CommissionableDeviceType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	config := func(t *testing.T) node.Config {
		t.Helper()
		return minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	}

	// inferDeviceType: RootNode and Aggregator recurse, the first device
	// type of a simple-class endpoint below them is the type
	// (ProductDescriptionServer.ts:99-125).
	t.Run("a bridged device's type is advertised", func(t *testing.T) {
		t.Parallel()
		_, adv := startAdvertising(t, config(t), newTypedDevice(t, "plug:1", onoff.DeviceTypeOnOffPlugInUnit), newTypedDevice(t, "light:1", onoff.DeviceTypeOnOffLight))
		if dt := advertisedDeviceType(t, adv); dt != uint64(onoff.DeviceTypeOnOffPlugInUnit) {
			t.Errorf("DT %d, want %d (the first bridged device)", dt, onoff.DeviceTypeOnOffPlugInUnit)
		}
	})

	// The class is the endpoint type's, Simple unless the type states
	// another (EndpointType.ts:31, `options.deviceClass ??
	// DeviceClassification.Simple`; only the node and utility types of
	// packages/node/src/endpoints/ state one): an endpoint whose device
	// type the snapshot does not hold is of the simple class, and its type
	// is the answer (ProductDescriptionServer.ts:109-112).
	t.Run("a device type outside the snapshot is advertised", func(t *testing.T) {
		t.Parallel()
		const unlisted = 0x0FF1 // no device type of parity/schema.json
		_, adv := startAdvertising(t, config(t), newTypedDevice(t, "custom:1", unlisted), newTypedDevice(t, "light:1", onoff.DeviceTypeOnOffLight))
		if dt := advertisedDeviceType(t, adv); dt != unlisted {
			t.Errorf("DT %d, want %d (the first bridged device, of the simple class)", dt, unlisted)
		}
	})

	// A device type the host states is not replaced (:44-46).
	t.Run("the host's type wins", func(t *testing.T) {
		t.Parallel()
		cfg := config(t)
		cfg.ProductDescription.DeviceType = 0x0101
		_, adv := startAdvertising(t, cfg, newTypedDevice(t, "plug:1", onoff.DeviceTypeOnOffPlugInUnit))
		if dt := advertisedDeviceType(t, adv); dt != 0x0101 {
			t.Errorf("DT %d, want the configured 257", dt)
		}
	})

	// Without an application endpoint matter.js infers nothing and tries
	// again on the next change of the endpoints (:55-62); the inference
	// runs again when the node next advertises. Once found, the type stays
	// (:44-46: the state is no longer unknown).
	t.Run("a device added later is found, and the first type found stays", func(t *testing.T) {
		t.Parallel()
		n, adv := startAdvertising(t, config(t))
		if dt := advertisedDeviceType(t, adv); dt != 0x0016 {
			t.Errorf("DT without an application endpoint %d, want RootNode (22)", dt)
		}
		if err := n.Aggregator().Add(ctx, newTypedDevice(t, "light:1", onoff.DeviceTypeOnOffLight)); err != nil {
			t.Fatal(err)
		}
		if err := n.OpenOwnWindow(ctx, 3*time.Minute); err != nil {
			t.Fatal(err)
		}
		if dt := advertisedDeviceType(t, adv); dt != uint64(onoff.DeviceTypeOnOffLight) {
			t.Errorf("DT after the device came %d, want %d", dt, onoff.DeviceTypeOnOffLight)
		}
		if err := n.Aggregator().Replace(ctx, endpoint.StringKey("light:1"), newTypedDevice(t, "light:1", onoff.DeviceTypeOnOffPlugInUnit)); err != nil {
			t.Fatal(err)
		}
		if err := n.OpenOwnWindow(ctx, 3*time.Minute); err != nil {
			t.Fatal(err)
		}
		if dt := advertisedDeviceType(t, adv); dt != uint64(onoff.DeviceTypeOnOffLight) {
			t.Errorf("DT after the device changed type %d, want the first found, %d", dt, onoff.DeviceTypeOnOffLight)
		}
	})
}
