// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The facade's read routes for a host that mounts its handlers on a
// running node (go-fabric-ui U1): Observe, Window, SetupPayload,
// NewWindowCredentials, the topology's clusters, SessionDiagnostics and
// Clock.

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/schema"
)

// TestObserveAfterStart: hooks registered on a started node see a
// commissioning and a fabric removal, after Config.Observers' hook and in
// registration order; an unsubscribed set sees nothing, and unsubscribe is
// idempotent.
func TestObserveAfterStart(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	var (
		mu     sync.Mutex
		events []string
	)
	record := func(s string) {
		mu.Lock()
		events = append(events, s)
		mu.Unlock()
	}
	cfg.Observers.OnFabricAdded = func(node.FabricInfo) error { record("config:added"); return nil }
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	removed := make(chan node.FabricInfo, 1)
	unsubscribe := n.Observe(node.Observers{
		OnFabricAdded:           func(node.FabricInfo) error { record("first:added"); return nil },
		OnCommissioningComplete: func(uint8) error { record("first:complete"); return nil },
		OnFabricRemoved:         func(info node.FabricInfo) error { record("first:removed"); removed <- info; return nil },
	})
	defer unsubscribe()
	n.Observe(node.Observers{OnFabricAdded: func(node.FabricInfo) error { record("second:added"); return nil }})
	gone := n.Observe(node.Observers{OnFabricAdded: func(node.FabricInfo) error { record("gone:added"); return nil }})
	gone()
	gone()

	opts := commissionerOptions()
	opts.Advertiser = adv
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	s, err := c.Commission(context.Background())
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	if status, err := c.RemoveFabric(context.Background(), s); err != nil || status != 0 {
		t.Fatalf("RemoveFabric = %d, %v", status, err)
	}
	select {
	case info := <-removed:
		if info.FabricIndex != c.FabricIndex() {
			t.Errorf("OnFabricRemoved fabric %d, want %d", info.FabricIndex, c.FabricIndex())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the registered OnFabricRemoved did not run")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"config:added", "first:added", "second:added", "first:complete", "first:removed"}
	if !slices.Equal(events, want) {
		t.Errorf("events %v, want %v", events, want)
	}
}

// TestWindowSnapshot: Window reports the node's own window with the
// configured discriminator and its timing, a basic window with the host's
// fresh credentials, and no window after a revoke — the value the window
// observer receives.
func TestWindowSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	seen := make(chan node.WindowSnapshot, 8)
	cfg.Observers.OnWindowTransition = func(s node.WindowSnapshot) error { seen <- s; return nil }
	n := newNode(t, cfg)
	if got := n.Window(); got.Kind != node.WindowNone || got.Open {
		t.Errorf("Window before Start = %+v, want none", got)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	own := n.Window()
	if !own.Open || own.Kind != node.WindowOwn || own.Discriminator != 3840 || own.CommissioningMode != 1 {
		t.Errorf("own window %+v, want open, own, discriminator 3840, CM=1", own)
	}
	if own.Timeout != bridge.OwnWindowTimeoutUncommissioned || !own.Deadline.Equal(own.OpenedAt.Add(own.Timeout)) {
		t.Errorf("own window timing opened %v deadline %v timeout %v, want deadline = opened + %v", own.OpenedAt, own.Deadline, own.Timeout, bridge.OwnWindowTimeoutUncommissioned)
	}
	if observed := <-seen; observed != own {
		t.Errorf("observer saw %+v, Window returns %+v", observed, own)
	}

	creds, err := node.NewWindowCredentials(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.OpenBasicWindow(ctx, creds); err != nil {
		t.Fatalf("OpenBasicWindow: %v", err)
	}
	basic := n.Window()
	if !basic.Open || basic.Kind != node.WindowBasic || basic.Discriminator != creds.Discriminator || basic.Timeout != time.Minute {
		t.Errorf("basic window %+v, want basic, discriminator %d, 1 min", basic, creds.Discriminator)
	}
	if err := n.CommissioningWindow().RevokeWindow(ctx); err != nil {
		t.Fatalf("RevokeWindow: %v", err)
	}
	// The node has no fabric, so its own window reopens after the basic
	// window closed; a second revoke leaves it closed.
	if got := n.Window(); got.Kind != node.WindowOwn {
		t.Fatalf("after closing the basic window = %+v, want the own window back", got)
	}
	if err := n.CommissioningWindow().RevokeWindow(ctx); err != nil {
		t.Fatalf("RevokeWindow: %v", err)
	}
	if got := n.Window(); got != (node.WindowSnapshot{Administrator: got.Administrator}) || got.Kind.String() != "none" {
		t.Errorf("after revoke = %+v, want no window", got)
	}
}

// TestSetupPayload: the configured secret with the node's vendor and
// product, and the codes PairingCodes returns.
func TestSetupPayload(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	n := newNode(t, cfg)
	p, err := n.SetupPayload()
	if err != nil {
		t.Fatal(err)
	}
	qr, manual, err := n.PairingCodes()
	if err != nil {
		t.Fatal(err)
	}
	want := node.SetupPayload{Discriminator: 3840, Passcode: testPasscode, VendorID: 0xFFF1, ProductID: 0x8001, QR: qr, Manual: manual}
	if p != want {
		t.Errorf("SetupPayload = %+v, want %+v", p, want)
	}
}

// TestNewWindowCredentials: every draw passes the setup-PIN rule and the
// window credential checks; two draws differ.
func TestNewWindowCredentials(t *testing.T) {
	t.Parallel()
	var prev node.WindowCredentials
	for i := range 200 {
		c, err := node.NewWindowCredentials(30 * time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := node.ValidateSetupPIN(c.Passcode); err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		if c.Discriminator > node.MaxDiscriminator || c.Iterations != node.PBKDFIterationsMin || len(c.Salt) != node.PBKDFSaltMax || c.Timeout != 30*time.Second {
			t.Fatalf("draw %d = %+v", i, c)
		}
		if i > 0 && c.Passcode == prev.Passcode && slices.Equal(c.Salt, prev.Salt) {
			t.Fatalf("draw %d repeats the previous one", i)
		}
		prev = c
	}
	for _, pin := range []uint32{0, 11111111, 12345678, 87654321, 99999999} {
		if node.ValidateSetupPIN(pin) == nil {
			t.Errorf("ValidateSetupPIN(%d) accepted a trivially-guessable passcode", pin)
		}
	}
	for range 200 {
		p, err := bridge.RandomPasscode()
		if err != nil || node.ValidateSetupPIN(p) != nil {
			t.Fatalf("bridge.RandomPasscode = %d, %v", p, err)
		}
	}
}

// TestTopologyClusters: every endpoint of the topology — a bridged node
// and its part — carries the clusters mounted on it with the schema's
// names and revisions, and its device type's revision.
func TestTopologyClusters(t *testing.T) {
	t.Parallel()
	owner, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("owner"), Name: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	part, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("owner/light"), Name: "light"})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AddPart(part.Device); err != nil {
		t.Fatal(err)
	}
	d := quietSite(t).AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{owner}})
	topo := d.Node().Topology()
	if len(topo.Devices) != 1 || len(topo.Devices[0].Parts) != 1 {
		t.Fatalf("Topology %+v, want the owner with one part", topo)
	}
	check := func(bd node.BridgedDevice, mustHave ...uint32) {
		t.Helper()
		if want, _ := schema.DeviceTypeRevision(schema.DeviceTypeID(bd.DeviceType)); bd.DeviceTypeRevision == 0 || bd.DeviceTypeRevision != want {
			t.Errorf("endpoint %d device type 0x%04X revision %d, want %d", bd.Endpoint, bd.DeviceType, bd.DeviceTypeRevision, want)
		}
		ids := make([]uint32, 0, len(bd.Clusters))
		for _, c := range bd.Clusters {
			ids = append(ids, c.ID)
			name, _ := schema.ClusterName(c.ID)
			rev, _ := schema.ClusterRevision(c.ID)
			if c.Name == "" || c.Name != name || c.Revision != rev {
				t.Errorf("endpoint %d cluster %+v, want name %q revision %d", bd.Endpoint, c, name, rev)
			}
		}
		for _, id := range mustHave {
			if !slices.Contains(ids, id) {
				t.Errorf("endpoint %d clusters %v lack 0x%04X", bd.Endpoint, ids, id)
			}
		}
	}
	const descriptor, bdbi, onOff = 0x001D, 0x0039, 0x0006
	check(topo.Devices[0], descriptor, bdbi)
	check(topo.Devices[0].Parts[0], descriptor, onOff)
}

// TestSessionDiagnostics: after a commissioning the node reports its
// sessions with their subscription counts and idle ages on its clock, and
// the occupancy adds up.
func TestSessionDiagnostics(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	n := newNode(t, cfg)
	if _, ok := n.Clock().(clock.System); !ok {
		t.Errorf("Clock() of a node without Config.Clock = %v, want clock.System", n.Clock())
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	if _, err := c.Commission(context.Background()); err != nil {
		t.Fatalf("Commission: %v", err)
	}
	d := n.SessionDiagnostics()
	if len(d.Sessions) == 0 {
		t.Fatal("no session after a commissioning")
	}
	if d.AsOf.IsZero() || d.AsOf.After(n.Clock().Now()) {
		t.Errorf("AsOf %v", d.AsOf)
	}
	counts := n.Subscriptions().CountBySession()
	for _, s := range d.Sessions {
		if s.Subscriptions != counts[s.ID] {
			t.Errorf("session %d subscriptions %d, want %d", s.ID, s.Subscriptions, counts[s.ID])
		}
		// AsOf is taken before the sessions are read: activity that lands in
		// between leaves LastActivity after AsOf, and Idle is clamped to zero
		// (ageAt) rather than negative.
		wantIdle := d.AsOf.Sub(s.LastActivity)
		if s.LastActivity.After(d.AsOf) {
			wantIdle = 0
		}
		if !s.LastActivity.IsZero() && s.Idle != wantIdle {
			t.Errorf("session %d idle %v, want %v (AsOf - LastActivity, clamped at zero)", s.ID, s.Idle, wantIdle)
		}
	}
	o := d.Occupancy
	if o.Live != len(d.Sessions) || o.Capacity == 0 || o.Free != o.Capacity-o.Live-o.Reserved {
		t.Errorf("Occupancy %+v with %d sessions", o, len(d.Sessions))
	}
}
