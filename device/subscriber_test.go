// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/node"
)

// reportSpy records the paths each report carries.
type reportSpy struct {
	mu    sync.Mutex
	calls [][]im.ConcreteAttributePath
}

func (s *reportSpy) report(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, paths)
}

func (s *reportSpy) reported() []im.ConcreteAttributePath {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []im.ConcreteAttributePath
	for _, c := range s.calls {
		out = append(out, c...)
	}
	return out
}

// dial is a host server that reads its level through a port and notifies
// when the port moves — a contract.ChangeNotifier the host never relays
// with WithChangeNotifier.
type dial struct {
	mu    sync.Mutex
	value uint8
	cbs   []func()
}

func (*dial) MatterClusterID() uint32 { return 0xFFF1_FC04 }

func (d *dial) MatterRead(attrID uint32) (any, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.value, attrID == 0
}

func (*dial) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*dial) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*dial) MatterReportable() []uint32                             { return []uint32{0} }

func (d *dial) OnMatterValueChanged(cb func()) func() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cbs = append(d.cbs, cb)
	return func() {}
}

func (d *dial) turn(v uint8) {
	d.mu.Lock()
	d.value = v
	cbs := slices.Clone(d.cbs)
	d.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

var _ contract.ChangeNotifier = (*dial)(nil)

// TestServerChangesReachASubscriberWithoutWithChangeNotifier: a default
// onoff.Server changed by Set, and a host server that notifies its own
// change, reach a subscriber of a device on a node although the device was
// built without WithChangeNotifier — the bridge hears every mounted
// server's notifier (bridge/subscribe.go), as matter.js reports each
// behavior's change through its own Datasource (Datasource.ts:776-796).
func TestServerChangesReachASubscriberWithoutWithChangeNotifier(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "node.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := node.New(node.Config{
		Network:       node.Network{Listen: "127.0.0.1:0", PreferIPv4: true, MDNS: node.MDNS{Disabled: true}},
		Commissioning: node.Commissioning{Passcode: 20202021, Discriminator: 3840, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{
			VendorID: 0xFFF1, ProductID: 0x8001,
		},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "device test", NodeLabel: "device test", SerialNumber: "DEVICE-TEST-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:  node.Store{DB: db},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })

	knob := &dial{}
	lamp, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("lamp"), Name: "Lamp"}, device.WithServer(knob))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := n.Aggregator().Add(ctx, lamp); err != nil {
		t.Fatal(err)
	}
	n.Aggregator().MarkComplete()
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var ep uint16
	for _, e := range n.Bridge().Topology().Bridged() {
		if e.SourceKey != nil && e.SourceKey.String() == "lamp" {
			ep = e.ID
		}
	}
	if ep == 0 {
		t.Fatal("lamp not assembled")
	}

	onOffPath := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: ep, Cluster: onoff.ClusterID, Attribute: onoff.AttrOnOff}
	knobPath := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: ep, Cluster: knob.MatterClusterID(), Attribute: 0}
	for name, c := range map[string]struct {
		path   im.ConcreteAttributePath
		change func()
	}{
		"onoff.Server Set": {onOffPath, func() { lamp.OnOff().Underlying().(*onoff.Server).Set(true) }},
		"host server":      {knobPath, func() { knob.turn(5) }},
	} {
		spy := &reportSpy{}
		mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
		if _, err := mgr.Subscribe(subscription.SubscribeArgs{
			PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
			AttributePaths: []im.ConcreteAttributePath{c.path},
		}); err != nil {
			t.Fatal(err)
		}
		n.Bridge().AttachSubscriptionManager(mgr)
		c.change()
		mgr.Tick(ctx, time.Now().Add(2*time.Second))
		if got := spy.reported(); !slices.Contains(got, c.path) {
			t.Errorf("%s: reported %v, want %v", name, got, c.path)
		}
	}
}
