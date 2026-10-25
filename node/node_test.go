// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
)

const testPasscode = 20202021

// openDB opens a file-backed SQLite database with the pragmas the daemon
// uses (examples/reference-bridge/persist.go:34).
func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// minimalConfig is the smallest configuration New accepts.
func minimalConfig(db *sql.DB) node.Config {
	return node.Config{
		Commissioning: node.Commissioning{Passcode: testPasscode, Discriminator: 3840, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{
			VendorID: 0xFFF1, ProductID: 0x8001,
		},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "node test", NodeLabel: "test node", SerialNumber: "NODE-TEST-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:  node.Store{DB: db},
		Logger: slog.New(slog.DiscardHandler),
		// A unit test publishes nothing on the network.
		Network: node.Network{Listen: "127.0.0.1:0", PreferIPv4: true, MDNS: node.MDNS{Disabled: true}},
	}
}

func newNode(t *testing.T, cfg node.Config) *node.Node {
	t.Helper()
	n, err := node.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n
}

// plug is a stub bridged OnOffPlugInUnit (0x010A) holding an onoff.Server.
type plug struct {
	key string
	srv *onoff.Server
}

func newPlug(t *testing.T, key string) *plug {
	t.Helper()
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	return &plug{key: key, srv: srv}
}

func (p *plug) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey:      endpoint.StringKey(p.key),
		DeviceAddress:  p.key,
		ChannelAddress: p.key + ":0",
		DeviceType:     onoff.DeviceTypeOnOffPlugInUnit,
		FriendlyName:   p.key,
		Source:         p,
	}
}

func (p *plug) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffPlugInUnit }

func (p *plug) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p.srv}
}

// endpointOf returns the endpoint number the live topology gave key.
func endpointOf(t *testing.T, n *node.Node, key string) (uint16, bool) {
	t.Helper()
	topo := n.Bridge().Topology()
	if topo == nil {
		return 0, false
	}
	for _, ep := range topo.Bridged() {
		if ep.SourceKey != nil && ep.SourceKey.String() == key {
			return ep.ID, true
		}
	}
	return 0, false
}

func uintList(v bridgetest.TLV) []uint64 {
	out := make([]uint64, 0, len(v.Children))
	for _, c := range v.Children {
		out = append(out, c.Uint)
	}
	return out
}

// TestNodeServesPASEReadAndSubscription is the facade's end-to-end proof
// that New wires the ports bridge/bridge.go lists: a commissioner
// completes PASE over UDP (the PASE provider and the node's own
// commissioning window, the session lookup, the ACK tracker), reads the
// root and a bridged endpoint through the dispatcher (root and Aggregator
// clusters, both PartsList providers), then commissions the node and
// holds a subscription over the CASE session that reports an Aggregator
// change (the subscription manager, Reassemble). The subscription rides
// CASE because a controller subscribes only to a commissioned peer
// (matter.js packages/protocol/src/action/client/ClientInteraction.ts:991-994
// at 0ceb202d).
func TestNodeServesPASEReadAndSubscription(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	ctx := context.Background()
	if err := n.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !n.CommissioningWindow().IsOpen() {
		t.Fatal("an uncommissioned node did not open its own window")
	}
	plugEP, ok := endpointOf(t, n, "plug:1")
	if !ok {
		t.Fatal("plug:1 not in the topology")
	}

	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	s, err := c.PASE(ctx)
	if err != nil {
		t.Fatalf("PASE: %v", err)
	}

	root, err := s.Read(ctx,
		attr(0, 0x0028, 0x0001), // BasicInformation VendorName
		attr(0, 0x0028, 0x0005), // NodeLabel
		attr(0, 0x001D, 0x0003), // root Descriptor PartsList
	)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	if got, _ := root.Value(0, 0x0028, 0x0001); got.String != "go-fabric test" {
		t.Errorf("VendorName %q", got.String)
	}
	if got, _ := root.Value(0, 0x0028, 0x0005); got.String != "test node" {
		t.Errorf("NodeLabel %q", got.String)
	}
	if got, _ := root.Value(0, 0x001D, 0x0003); !slices.Equal(uintList(got), []uint64{1, uint64(plugEP)}) {
		t.Errorf("root PartsList %v, want [1 %d]", uintList(got), plugEP)
	}
	bridged, err := s.Read(ctx, attr(plugEP, onoff.ClusterID, onoff.AttrOnOff))
	if err != nil {
		t.Fatalf("read bridged: %v", err)
	}
	if v, ok := bridged.Value(plugEP, onoff.ClusterID, onoff.AttrOnOff); !ok || v.Bool {
		t.Errorf("bridged OnOff %+v (%v), want false", v, ok)
	}

	s.Close()
	s, err = c.Commission(ctx)
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	sub, err := s.Subscribe(ctx, 0, 60, attr(1, 0x001D, 0x0003))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got, _ := sub.Priming.Value(1, 0x001D, 0x0003); !slices.Equal(uintList(got), []uint64{uint64(plugEP)}) {
		t.Errorf("priming Aggregator PartsList %v", uintList(got))
	}
	if sub.ID == 0 {
		t.Fatal("SubscribeResponse without a SubscriptionID")
	}

	if err := n.Aggregator().Add(ctx, newPlug(t, "plug:2")); err != nil {
		t.Fatalf("Add after Start: %v", err)
	}
	plug2, ok := endpointOf(t, n, "plug:2")
	if !ok {
		t.Fatal("plug:2 not reassembled into the topology")
	}
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		report, err := s.NextReport(deadline)
		if err != nil {
			t.Fatalf("no report after the Aggregator changed: %v", err)
		}
		if v, ok := report.Value(1, 0x001D, 0x0003); ok {
			if got := uintList(v); !slices.Equal(got, []uint64{uint64(plugEP), uint64(plug2)}) {
				t.Errorf("reported PartsList %v", got)
			}
			break
		}
	}
}

// TestStartStopIdempotent: Start twice is one start (matter.js Node.start,
// Node.ts:114-132), Stop twice one stop, and a stopped node does not
// start again.
func TestStartStopIdempotent(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	ctx := context.Background()
	for range 2 {
		if err := n.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
	}
	for range 2 {
		if err := n.Stop(ctx); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}
	if err := n.Start(ctx); !errors.Is(err, node.ErrStopped) {
		t.Errorf("Start after Stop: %v", err)
	}
}

// TestStopWithoutStart releases what New began.
func TestStopWithoutStart(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestRunStopsWithTheContext: Run starts the node and stops it when its
// context ends (matter.js Node.run, Node.ts:183-186).
func TestRunStopsWithTheContext(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- n.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for n.Bridge().Topology() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
	if err := n.Start(context.Background()); !errors.Is(err, node.ErrStopped) {
		t.Errorf("Start after Run: %v", err)
	}
}

// TestAggregatorAcrossRestart: Add before and after Start, Remove and
// Forget on a complete set, and a restart on the same database. An
// endpoint kept across the restart keeps its number
// (BD-Matter-EndpointID-Persistent); one removed keeps its reservation
// across the restart and gets its number back, as a closed matter.js
// endpoint does (packages/node/src/node/server/ServerEndpointInitializer.ts:
// 67-69 at 862ad75f); one forgotten and added again gets a fresh one, as
// the allocator never reissues a released number
// (BD-Matter-EndpointNumber-Monotonic).
//
// matter.js's delete() erases the endpoint's storage
// (packages/node/src/endpoint/Endpoint.ts:987-992).
func TestAggregatorAcrossRestart(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "node.db")
	ctx := context.Background()

	n := newNode(t, minimalConfig(openDB(t, path)))
	a, b, c := newPlug(t, "plug:a"), newPlug(t, "plug:b"), newPlug(t, "plug:c")
	if err := n.Aggregator().Add(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, c); err != nil {
		t.Fatal(err)
	}
	n.Aggregator().MarkComplete()
	idA, okA := endpointOf(t, n, "plug:a")
	idB, okB := endpointOf(t, n, "plug:b")
	idC, okC := endpointOf(t, n, "plug:c")
	if !okA || !okB || !okC || idA == idB || idB == idC {
		t.Fatalf("endpoints a=%d (%v) b=%d (%v) c=%d (%v)", idA, okA, idB, okB, idC, okC)
	}
	if got := len(n.Aggregator().Endpoints()); got != 3 {
		t.Errorf("Endpoints() has %d entries", got)
	}
	if err := n.Aggregator().Remove(ctx, endpoint.StringKey("plug:a")); err != nil {
		t.Fatal(err)
	}
	if _, ok := endpointOf(t, n, "plug:a"); ok {
		t.Error("removed endpoint still in the topology")
	}
	if err := n.Aggregator().Forget(ctx, endpoint.StringKey("plug:c")); err != nil {
		t.Fatal(err)
	}
	if _, ok := endpointOf(t, n, "plug:c"); ok {
		t.Error("forgotten endpoint still in the topology")
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	m := newNode(t, minimalConfig(openDB(t, path)))
	if err := m.Aggregator().Add(ctx, newPlug(t, "plug:b")); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// A complete set without plug:a: its reservation outlives the
	// restart and the collection.
	m.Aggregator().MarkComplete()
	if err := m.Aggregator().Add(ctx, newPlug(t, "plug:a")); err != nil {
		t.Fatal(err)
	}
	if err := m.Aggregator().Add(ctx, newPlug(t, "plug:c")); err != nil {
		t.Fatal(err)
	}
	if got, _ := endpointOf(t, m, "plug:b"); got != idB {
		t.Errorf("plug:b endpoint %d after the restart, want %d", got, idB)
	}
	if got, _ := endpointOf(t, m, "plug:a"); got != idA {
		t.Errorf("re-added plug:a endpoint %d, want its reserved number %d", got, idA)
	}
	if got, _ := endpointOf(t, m, "plug:c"); got == idC || got == 0 {
		t.Errorf("re-added plug:c endpoint %d, want a fresh number (was %d)", got, idC)
	}
}

// TestAggregatorRefusals: a missing or duplicate key, an unknown removal.
func TestAggregatorRefusals(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	ctx := context.Background()
	agg := n.Aggregator()
	if err := agg.Add(ctx, keyless{}); !errors.Is(err, node.ErrNoStableKey) {
		t.Errorf("keyless Add: %v", err)
	}
	if err := agg.Add(ctx, newPlug(t, "p")); err != nil {
		t.Fatal(err)
	}
	if err := agg.Add(ctx, newPlug(t, "p")); !errors.Is(err, node.ErrDuplicateKey) {
		t.Errorf("duplicate Add: %v", err)
	}
	if err := agg.Remove(ctx, endpoint.StringKey("q")); !errors.Is(err, node.ErrUnknownKey) {
		t.Errorf("unknown Remove: %v", err)
	}
	if err := agg.Remove(ctx, nil); !errors.Is(err, node.ErrNoStableKey) {
		t.Errorf("nil Remove: %v", err)
	}
	if err := agg.Remove(ctx, endpoint.StringKey("p")); err != nil {
		t.Errorf("Remove before Start: %v", err)
	}
	if err := agg.IncreaseConfigurationVersion(endpoint.StringKey("p")); err == nil {
		t.Error("IncreaseConfigurationVersion before Start succeeded")
	}
}

type keyless struct{}

func (keyless) Spec() endpoint.Spec { return endpoint.Spec{} }

// TestPersistedLabelAndConfigurationVersion: a NodeLabel the store holds
// for a bridged endpoint is served after a start, and a raised
// ConfigurationVersion is persisted and served after a restart — the
// daemon's labels / configVersions (persist.go).
func TestPersistedLabelAndConfigurationVersion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "node.db")
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, path)))
	if err := n.Store().SetSetting(ctx, "bridged_node_label.plug:1", "Kitchen"); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ep, _ := endpointOf(t, n, "plug:1")
	if label := n.Bridge().Topology().FindByID(ep).NodeLabel(); label != "Kitchen" {
		t.Errorf("bridged NodeLabel %q", label)
	}
	before := n.Bridge().Topology().FindByID(ep).ConfigurationVersion()
	if err := n.Aggregator().IncreaseConfigurationVersion(endpoint.StringKey("plug:1")); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().IncreaseConfigurationVersion(endpoint.StringKey("nope")); !errors.Is(err, node.ErrUnknownKey) {
		t.Errorf("unknown key: %v", err)
	}
	raised := n.Bridge().Topology().FindByID(ep).ConfigurationVersion()
	if raised == before {
		t.Fatalf("ConfigurationVersion %v not raised", raised)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	m := newNode(t, minimalConfig(openDB(t, path)))
	if err := m.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m.Bridge().Topology().FindByID(ep).ConfigurationVersion(); got != raised {
		t.Errorf("ConfigurationVersion %v after the restart, want %v", got, raised)
	}
}

// readAttr reads one attribute of the root or the Aggregator off the live
// topology's attached cluster set.
func readAttr(t *testing.T, topo *endpoint.Topology, ep uint16, cluster, attr uint32) any {
	t.Helper()
	e := topo.FindByID(ep)
	if e == nil {
		t.Fatalf("endpoint %d missing", ep)
	}
	for _, srv := range e.AttachedClusterServers() {
		if srv.MatterClusterID() == cluster {
			v, _ := srv.MatterRead(attr)
			return v
		}
	}
	t.Fatalf("endpoint %d has no cluster 0x%04X", ep, cluster)
	return nil
}

// TestPairingCodes: the codes of the configured passcode.
func TestPairingCodes(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	qr, manual, err := n.PairingCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(qr) < 4 || qr[:3] != "MT:" || len(manual) != 11 {
		t.Errorf("qr %q manual %q", qr, manual)
	}
	if fabrics, err := n.FabricInfos(context.Background()); err != nil || len(fabrics) != 0 {
		t.Errorf("fabrics %v %v", fabrics, err)
	}
	if n.Root().BasicInformation == nil || len(n.Root().Servers()) == 0 {
		t.Error("root servers missing")
	}
}

// TestConfigValidate holds the daemon's flag checks (main.go:113-131).
func TestConfigValidate(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	cases := []struct {
		name string
		mod  func(*node.Config)
	}{
		{"discriminator", func(c *node.Config) { c.Commissioning.Discriminator = 0x1000 }},
		{"passcode trivial", func(c *node.Config) { c.Commissioning.Passcode = 11111111 }},
		{"passcode range", func(c *node.Config) { c.Commissioning.Passcode = 99999999 }},
		{"iterations low", func(c *node.Config) { c.Commissioning.PBKDFIterations = 999 }},
		{"iterations high", func(c *node.Config) { c.Commissioning.PBKDFIterations = 100001 }},
		{"salt short", func(c *node.Config) { c.Commissioning.Salt = []byte("short") }},
		{"vendor", func(c *node.Config) { c.ProductDescription.VendorID = 0 }},
		{"label", func(c *node.Config) { c.BasicInformation.NodeLabel = " " }},
		{"store", func(c *node.Config) { c.Store = node.Store{} }},
		{"ota dir", func(c *node.Config) { c.OTA = &node.OtaConfig{} }},
	}
	for _, c := range cases {
		cfg := minimalConfig(db)
		c.mod(&cfg)
		if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
			t.Errorf("%s: %v", c.name, err)
		}
		if _, err := node.New(cfg); !errors.Is(err, node.ErrInvalidConfig) {
			t.Errorf("%s: New accepted it (%v)", c.name, err)
		}
	}
	cfg := minimalConfig(db)
	if err := cfg.Validate(); err != nil {
		t.Errorf("minimal config: %v", err)
	}
}

// TestOTARequestorMounted: Config.OTA mounts the requestor and the
// OtaRequestor device type on the root.
func TestOTARequestorMounted(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.OTA = &node.OtaConfig{Dir: filepath.Join(t.TempDir(), "ota")}
	n := newNode(t, cfg)
	if n.Root().OTARequestor == nil {
		t.Fatal("no OTA requestor")
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	dt := readAttr(t, n.Bridge().Topology(), 0, 0x001D, 0x0000)
	if dt == nil {
		t.Error("root DeviceTypeList empty")
	}
}

// TestMarkComplete: a node started with an empty Aggregator keeps the
// number of an endpoint stored by a previous run until MarkComplete, so
// one added after Start gets its old number back; once MarkComplete was
// called, a stored endpoint absent from the set is garbage-collected and
// a later add gets a fresh number.
func TestMarkComplete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	seed := func(t *testing.T, path string) (idA uint16) {
		t.Helper()
		n := newNode(t, minimalConfig(openDB(t, path)))
		for _, k := range []string{"plug:a", "plug:b"} {
			if err := n.Aggregator().Add(ctx, newPlug(t, k)); err != nil {
				t.Fatal(err)
			}
		}
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		n.Aggregator().MarkComplete()
		n.Aggregator().MarkComplete() // idempotent
		idA, _ = endpointOf(t, n, "plug:a")
		if err := n.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		return idA
	}

	t.Run("retained until MarkComplete", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "node.db")
		idA := seed(t, path)
		m := newNode(t, minimalConfig(openDB(t, path)))
		if err := m.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if err := m.Aggregator().Add(ctx, newPlug(t, "plug:a")); err != nil {
			t.Fatal(err)
		}
		if got, _ := endpointOf(t, m, "plug:a"); got != idA {
			t.Errorf("plug:a endpoint %d after Start, want its stored %d", got, idA)
		}
	})

	t.Run("collected after MarkComplete", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "node.db")
		idA := seed(t, path)
		m := newNode(t, minimalConfig(openDB(t, path)))
		if err := m.Aggregator().Add(ctx, newPlug(t, "plug:b")); err != nil {
			t.Fatal(err)
		}
		if err := m.Start(ctx); err != nil {
			t.Fatal(err)
		}
		m.Aggregator().MarkComplete()
		if err := m.Aggregator().Add(ctx, newPlug(t, "plug:a")); err != nil {
			t.Fatal(err)
		}
		if got, _ := endpointOf(t, m, "plug:a"); got == idA || got == 0 {
			t.Errorf("plug:a endpoint %d, want a fresh number (stored %d was collected)", got, idA)
		}
	})
}

// TestBootReason: the node reports the BootReason its GeneralDiagnostics
// emits — PowerOnReboot (1) for a node the facade builds.
func TestBootReason(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	if got := n.BootReason(); got != mattercore.BootReasonPowerOnReboot {
		t.Errorf("BootReason %d, want %d", got, mattercore.BootReasonPowerOnReboot)
	}
	srv, err := onoff.New(onoff.Config{
		Features: onoff.FeatureLT, BootReason: n.BootReason,
		Initial: map[uint32]any{onoff.AttrStartUpOnOff: onoff.StartUpOn},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !srv.OnOff() {
		t.Error("StartUpOnOff not applied after a power-on reboot")
	}
}

// TestLocales: Config.Locales are the root LocalizationConfiguration's
// SupportedLocales and the first is its ActiveLocale; nil serves en-US
// alone.
func TestLocales(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		locales   []string
		active    string
		supported []string
	}{
		"default": {nil, "en-US", []string{"en-US"}},
		"host's":  {[]string{"en-US", "de-DE"}, "en-US", []string{"en-US", "de-DE"}},
	} {
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
		cfg.Locales = tc.locales
		n := newNode(t, cfg)
		var found bool
		for _, srv := range n.Root().Servers() {
			if srv.MatterClusterID() != 0x002B {
				continue
			}
			found = true
			if v, _ := srv.MatterRead(0x0000); v != tc.active {
				t.Errorf("%s: ActiveLocale %v, want %s", name, v, tc.active)
			}
			if v, _ := srv.MatterRead(0x0001); !slices.Equal(v.([]string), tc.supported) {
				t.Errorf("%s: SupportedLocales %v, want %v", name, v, tc.supported)
			}
		}
		if !found {
			t.Errorf("%s: no LocalizationConfiguration on the root", name)
		}
	}
}

// TestAggregatorConcurrentAdds: devices added from many goroutines at
// once all end in the topology — reassembly is serialised, so an older
// snapshot never installs after a newer one.
func TestAggregatorConcurrentAdds(t *testing.T) {
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	n.Aggregator().MarkComplete()
	const count = 16
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := n.Aggregator().Add(ctx, newPlug(t, fmt.Sprintf("plug:%02d", i))); err != nil {
				t.Errorf("Add %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	for i := range count {
		if _, ok := endpointOf(t, n, fmt.Sprintf("plug:%02d", i)); !ok {
			t.Errorf("plug:%02d missing from the topology after concurrent adds", i)
		}
	}
}
