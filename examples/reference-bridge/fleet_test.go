// SPDX-License-Identifier: MIT
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
// So this file boots a real [matterbridge.Bridge] over the daemon's own
// fleet — the same snapshotter main() passes — and drives changes in at the
// device end, where a southbound event would arrive.
package main

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/modeselect"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
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
			// Subscribe stamps lastReport at admission and the manager
			// floors MinIntervalFloor to one second, so the dirty-path drain
			// gate needs `now` to be that far past it.
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

// startFleetBridge boots the daemon's fleet behind a real bridge on an
// ephemeral port, with an in-memory endpoint store and no mDNS.
//
// It goes through [newFleet] and [matterbridge.New] rather than assembling a
// topology by hand: the point of the test is the wiring main() performs, and
// a topology built here would prove only that one can be built.
func startFleetBridge(t *testing.T) (*fleet, *matterbridge.Bridge) {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)

	f, err := newFleet(endpointtest.NewFakeStore(), endpoint.Config{
		VendorID:  testVendorID,
		ProductID: testProductID,
		NodeLabel: "fleet-test",
	}, logger)
	if err != nil {
		t.Fatalf("newFleet: %v", err)
	}

	br, err := matterbridge.New(f.snapshotter, mdns.NewNoop(), matterbridge.Config{
		Listen:    "127.0.0.1:0",
		VendorID:  testVendorID,
		ProductID: testProductID,
		NodeLabel: "fleet-test",
	}, logger)
	if err != nil {
		t.Fatalf("bridge.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = br.Stop(stopCtx)
	})
	if err := br.Start(ctx); err != nil {
		t.Fatalf("bridge.Start: %v", err)
	}
	return f, br
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
