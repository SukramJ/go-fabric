// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The guard over this host's mounted endpoints.
//
// Every cluster server in the module can be tested by handing it a fake port
// and calling its methods; what that cannot show is whether a host's device
// ever reaches a controller. The path in between belongs to the bridge: the
// assembler materialises the endpoint's cluster servers, the bridge
// subscribes to the endpoint source's change notifier at mount time, reads
// the reportable attribute paths back off the mounted servers, and marks
// them dirty on the subscription manager. A device wired to a server nobody
// mounts satisfies every cluster test and reports nothing.
//
// So this file boots a real node over the daemon's own fleet — the same
// assembly main() performs — and drives changes in at the device end, where
// a southbound event would arrive.
package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/cluster/modeselect"
	bcdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/node"
)

// TestDeviceSideChangeReachesASubscriberThroughTheMountedEndpoint drives one
// change per device at the device end and asserts it arrives twice over: as
// a dirty path on a subscription the manager holds, and as the new value on
// the cluster server the assembler mounted.
//
// Both halves are needed. The read alone would pass on an endpoint nothing
// subscribes to; the dirty path alone would pass on a notifier that fires
// without the device having moved.
func TestDeviceSideChangeReachesASubscriberThroughTheMountedEndpoint(t *testing.T) {
	f, br := startFleetBridge(t)

	cases := []struct {
		name string
		// deviceAddress is how the endpoint is found in the assembled
		// topology, so the test never assumes an endpoint number.
		deviceAddress string
		clusterID     uint32
		attributeID   uint32
		before        any
		after         any
		// change happens at the device, not through a Matter command.
		change func(t *testing.T)
	}{
		{
			name:          "valve reports the head it moved by itself",
			deviceAddress: "demo-valve-1",
			clusterID:     valve.ClusterID,
			attributeID:   valve.AttrCurrentState,
			before:        uint8(valve.StateClosed),
			after:         uint8(valve.StateOpen),
			change:        func(*testing.T) { f.valve.reportFromDevice(valve.StateOpen) },
		},
		{
			name:          "selector reports the knob a person turned",
			deviceAddress: "demo-selector-1",
			clusterID:     modeselect.ClusterID,
			attributeID:   modeselect.AttrCurrentMode,
			before:        brewNormal,
			after:         brewStrong,
			change: func(t *testing.T) {
				t.Helper()
				if err := f.selector.reportFromDevice(brewStrong); err != nil {
					t.Fatalf("selector.reportFromDevice: %v", err)
				}
			},
		},
		{
			name:          "speaker reports its own front-panel dial",
			deviceAddress: "demo-speaker-1",
			clusterID:     levelcontrol.ClusterID,
			attributeID:   levelcontrol.AttrCurrentLevel,
			before:        uint8(120),
			after:         uint8(200),
			change:        func(*testing.T) { f.speaker.reportFromDevice(200) },
		},
		{
			name:          "contact sensor reports the window it saw open",
			deviceAddress: "demo-contact-1",
			clusterID:     measurement.ClusterBooleanState,
			attributeID:   0x0000, // StateValue
			before:        true,
			after:         false,
			change:        func(*testing.T) { f.contact.reportFromDevice(false) },
		},
		{
			// A sensor on the device layer (device.WithMeasurement): the
			// assembler materialises its cluster from the reading, and the
			// bridge hears the reading's own notifier.
			name:          "humidity sensor reports its new reading",
			deviceAddress: "demo-humidity-1",
			clusterID:     0x0405, // RelativeHumidityMeasurement
			attributeID:   0x0000, // MeasuredValue
			before:        uint16(4800),
			after:         uint16(5500),
			change:        func(*testing.T) { f.humidity.reportFromDevice(55) },
		},
		{
			name:          "contact sensor raises its visual alarm",
			deviceAddress: "demo-contact-1",
			clusterID:     boolcfg.ClusterID,
			attributeID:   0x0003, // AlarmsActive
			before:        uint8(0),
			after:         uint8(boolcfg.AlarmVisual),
			change: func(t *testing.T) {
				t.Helper()
				visual := boolcfg.AlarmVisual
				if err := f.contact.config.Set(bcdef.Patch{AlarmsActive: &visual}); err != nil {
					t.Fatalf("SetAlarmsActive: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		// Deliberately not parallel: each subtest attaches its own
		// subscription manager, and attaching one re-wires the bridge's
		// notifier listeners for every endpoint.
		t.Run(tc.name, func(t *testing.T) {
			ep := mountedEndpoint(t, br, tc.deviceAddress)
			path := im.ConcreteAttributePath{
				HasEndpoint:  true,
				HasCluster:   true,
				HasAttribute: true,
				Endpoint:     ep.ID,
				Cluster:      tc.clusterID,
				Attribute:    tc.attributeID,
			}

			if got := readMounted(t, ep, tc.clusterID, tc.attributeID); got != tc.before {
				t.Fatalf("endpoint %d cluster 0x%04X attribute 0x%04X = %v (%T) before the change, want %v",
					ep.ID, tc.clusterID, tc.attributeID, got, got, tc.before)
			}

			spy := &reporterSpy{}
			mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
			if _, err := mgr.Subscribe(subscription.SubscribeArgs{
				FabricIndex:        0,
				PeerNodeID:         1,
				SessionID:          1,
				MinIntervalFloor:   0,
				MaxIntervalCeiling: 60,
				AttributePaths:     []im.ConcreteAttributePath{path},
			}); err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			br.AttachSubscriptionManager(mgr)

			tc.change(t)

			// Drive the engine with a wall-clock offset rather than waiting:
			// Subscribe stamps lastReport at admission; a tick well past it
			// drains the dirty paths whatever floor applies.
			mgr.Tick(context.Background(), time.Now().Add(2*time.Second))

			paths := spy.reported()
			if len(paths) != 1 {
				t.Fatalf("subscriber received %d reports, want 1: the change at the device did not reach the subscription "+
					"(endpoint %d, cluster 0x%04X, attribute 0x%04X)",
					len(paths), ep.ID, tc.clusterID, tc.attributeID)
			}
			if len(paths[0]) != 1 || paths[0][0] != path {
				t.Errorf("reported paths = %+v, want exactly %+v", paths[0], path)
			}

			if got := readMounted(t, ep, tc.clusterID, tc.attributeID); got != tc.after {
				t.Errorf("endpoint %d cluster 0x%04X attribute 0x%04X = %v (%T) after the change, want %v",
					ep.ID, tc.clusterID, tc.attributeID, got, got, tc.after)
			}
		})
	}
}

// startFleetBridge boots the daemon's fleet on a node on an ephemeral
// port, with a throw-away database and no mDNS, and returns its bridge.
//
// It goes through [newFleet] and [assemble], the wiring main() performs,
// rather than assembling a topology by hand: the point of the test is that
// wiring, and a topology built here would prove only that one can be built.
func startFleetBridge(t *testing.T) (*fleet, *matterbridge.Bridge) {
	t.Helper()
	f, n := startFleetNode(t)
	return f, n.Bridge()
}

// startFleetNode is [startFleetBridge] returning the node.
func startFleetNode(t *testing.T) (*fleet, *node.Node) {
	t.Helper()
	f := newFleet()
	n := testNode(t, f, filepath.Join(t.TempDir(), "fleet.db"))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := n.Start(ctx); err != nil {
		t.Fatalf("node.Start: %v", err)
	}
	// As main does once the node has numbered the parts.
	if err := f.numberPowerSources(); err != nil {
		t.Fatalf("numberPowerSources: %v", err)
	}
	return f, n
}

// testNode assembles f on a node over the database at path, as main()
// does, unstarted; the node is stopped when the test ends.
func testNode(t *testing.T, f *fleet, path string) *node.Node {
	t.Helper()
	return testNodeWith(t, f, path, nil)
}

// testNodeWith is testNode with the node configuration amended by edit
// (nil: none) before the node is built.
func testNodeWith(t *testing.T, f *fleet, path string, edit func(*node.Config)) *node.Node {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	applier := &otaApplier{
		vendorID: testVendorID, productID: testProductID,
		dir: filepath.Join(filepath.Dir(path), "ota"), logger: logger,
	}
	actions, err := mattercore.NewActions(mattercore.ActionsConfig{Host: f.actions})
	if err != nil {
		t.Fatalf("NewActions: %v", err)
	}
	// advertise is false: the node publishes nothing (Network.MDNS.Disabled).
	cfg := nodeConfig(flags{dbPath: path, listen: "127.0.0.1:0", label: "fleet-test", salt: "go-fabric-ref-salt", iterations: 1000},
		20202021, 3840, node.TCPConfig{}, applier, actions, logger)
	if edit != nil {
		edit(&cfg)
	}
	n, err := assemble(context.Background(), f, cfg, applier)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = n.Stop(stopCtx)
	})
	return n
}

// mountedEndpoint finds the bridged endpoint the assembler gave the named
// device. The endpoint number is discovered rather than assumed: it is the
// store's to assign.
func mountedEndpoint(t *testing.T, br *matterbridge.Bridge, deviceAddress string) *endpoint.Endpoint {
	t.Helper()
	topology := br.Topology()
	if topology == nil {
		t.Fatal("bridge has no topology after Start")
	}
	for _, ep := range topology.Endpoints {
		if ep != nil && ep.DeviceAddress == deviceAddress {
			return ep
		}
	}
	t.Fatalf("no endpoint in the assembled topology carries device address %q", deviceAddress)
	return nil
}

// readMounted reads one attribute off the cluster server the assembler
// mounted on ep — not off a server the test constructed.
func readMounted(t *testing.T, ep *endpoint.Endpoint, clusterID, attrID uint32) any {
	t.Helper()
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv == nil || srv.MatterClusterID() != clusterID {
			continue
		}
		value, ok := srv.MatterRead(attrID)
		if !ok {
			t.Fatalf("endpoint %d cluster 0x%04X does not serve attribute 0x%04X", ep.ID, clusterID, attrID)
		}
		return value
	}
	t.Fatalf("endpoint %d mounts no cluster 0x%04X", ep.ID, clusterID)
	return nil
}

// reporterSpy records the dirty-path reports the subscription engine hands
// to its [subscription.Reporter], which is where a real bridge encodes and
// sends a ReportData.
type reporterSpy struct {
	mu    sync.Mutex
	calls [][]im.ConcreteAttributePath
}

func (s *reporterSpy) report(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, paths)
}

func (s *reporterSpy) reported() [][]im.ConcreteAttributePath {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]im.ConcreteAttributePath(nil), s.calls...)
}

// TestBridgedDevicesServeTheNodesVendorName: the demo devices have no
// manufacturer of their own, so every bridged endpoint serves the node's
// VendorName — non-empty, as Apple Home needs the four vendor fields.
func TestBridgedDevicesServeTheNodesVendorName(t *testing.T) {
	_, br := startFleetBridge(t)
	topo := br.Topology()
	if topo == nil || len(topo.Bridged()) == 0 {
		t.Fatal("the fleet mounted no bridged endpoint")
	}
	for _, ep := range topo.Bridged() {
		if got := readMounted(t, ep, 0x0039, 0x0001); got != exampleVendorName {
			t.Errorf("endpoint %d VendorName = %v, want %q", ep.ID, got, exampleVendorName)
		}
	}
}

// TestSmokeAlarmHasItsPowerSourcePart: the SmokeCoAlarm's PowerSource
// component is a part of the alarm endpoint (matter.js
// examples/device-smoke-co-alarm), so the device-type validator no longer
// reports the missing component.
func TestSmokeAlarmHasItsPowerSourcePart(t *testing.T) {
	_, br := startFleetBridge(t)
	topo := br.Topology()
	var alarm *endpoint.Endpoint
	for _, ep := range topo.Bridged() {
		if ep.DeviceType == 0x0076 {
			alarm = ep
		}
	}
	if alarm == nil || len(alarm.PartIDs) != 1 {
		t.Fatalf("smoke alarm %+v: want one part", alarm)
	}
	if part := topo.FindByID(alarm.PartIDs[0]); part == nil || part.DeviceType != deviceTypePowerSource {
		t.Fatalf("the alarm's part is %+v, want a PowerSource endpoint", part)
	}
	for _, v := range endpoint.ValidateDeviceTypes(topo) {
		if v.Endpoint == alarm.ID || v.Endpoint == alarm.PartIDs[0] {
			t.Errorf("device type violation: %s", v)
		}
	}
}

// TestAirQualitySensorMountsEveryConcentrationCluster: the AirQualitySensor
// endpoint carries AirQuality, which the device type mandates, and the ten
// concentration clusters it lists as optional, each exactly once.
func TestAirQualitySensorMountsEveryConcentrationCluster(t *testing.T) {
	_, br := startFleetBridge(t)
	ep := mountedEndpoint(t, br, "demo-airquality-1")
	want := []uint32{
		measurement.ClusterAirQuality,
		measurement.ClusterCO2Concentration, measurement.ClusterCOConcentration,
		measurement.ClusterNO2Concentration, measurement.ClusterOzoneConcentration,
		measurement.ClusterPM25Concentration, measurement.ClusterFormaldehydeConcentration,
		measurement.ClusterPM1Concentration, measurement.ClusterPM10Concentration,
		measurement.ClusterTVOCConcentration, measurement.ClusterRadonConcentration,
	}
	seen := map[uint32]int{}
	for _, srv := range endpoint.ClusterServers(ep) {
		seen[srv.MatterClusterID()]++
	}
	for _, id := range want {
		if seen[id] != 1 {
			t.Errorf("cluster 0x%04X mounted %d times, want once", id, seen[id])
		}
	}
	if ep.DeviceType != 0x002C {
		t.Errorf("device type 0x%04X, want AirQualitySensor", ep.DeviceType)
	}
}

// TestButtonPressWiringThroughTheDeviceLayer: the button is a
// device.NewGenericSwitch on its declaration, and a press reaches the
// kit's Switch server through its typed interface.
func TestButtonPressWiringThroughTheDeviceLayer(t *testing.T) {
	f, br := startFleetBridge(t)
	mountedEndpoint(t, br, "demo-button-1")
	if err := f.button.simulateMultiPress(1, 1, 0, 0); err != nil {
		t.Fatalf("the button has no Switch position input wired: a press reaches no controller (%v)", err)
	}
}
