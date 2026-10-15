// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The seams a bridge host with its own topology assembler and its own
// operator surface needs to adopt node.New (issue #87): a host-assembled
// topology, the UniqueID pin, the session and subscription diagnostics,
// host-initiated fabric removal and LocationCapability.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// centralKey is a host's composite stable key, the shape a per-central
// assembler uses instead of endpoint.StringKey.
type centralKey struct{ central, device string }

func (k centralKey) String() string { return k.central + "/" + k.device }

// TestHostTopology: with Config.Topology the bridge serves the host's own
// snapshotter — two scopes, each with its own completeness, host key
// types — the facade builds no Aggregator, and the root's and Aggregator's
// PartsList providers still see the host's endpoints.
func TestHostTopology(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	if err := sqlitestore.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := minimalConfig(db)
	asm, err := endpoint.New(sqlitestore.New(db), endpoint.Config{
		VendorID: cfg.ProductDescription.VendorID, ProductID: cfg.ProductDescription.ProductID,
		NodeLabel: cfg.BasicInformation.NodeLabel,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		calls int
	)
	plugA, plugB := newPlug(t, "a:1"), newPlug(t, "b:1")
	specOf := func(p *plug, key centralKey) endpoint.Spec {
		spec := p.Spec()
		spec.StableKey = key
		return spec
	}
	cfg.Topology = func(ctx context.Context) (*endpoint.Topology, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return asm.Assemble(ctx, []endpoint.Snapshot{
			{Scope: "ccu-a", Endpoints: []endpoint.Spec{specOf(plugA, centralKey{"ccu-a", "plug:1"})}, ModelComplete: true},
			{Scope: "ccu-b", Endpoints: []endpoint.Spec{specOf(plugB, centralKey{"ccu-b", "plug:1"})}, ModelComplete: false},
		})
	}
	n := newNode(t, cfg)
	if n.Aggregator() != nil {
		t.Fatal("a host-assembled node still has an Aggregator")
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mu.Lock()
	if calls != 1 {
		t.Errorf("snapshotter called %d times by Start, want 1", calls)
	}
	mu.Unlock()
	epA, okA := endpointOf(t, n, "ccu-a/plug:1")
	epB, okB := endpointOf(t, n, "ccu-b/plug:1")
	if !okA || !okB {
		t.Fatalf("host endpoints not in the topology (%v, %v)", okA, okB)
	}
	s := paseSession(t, n)
	report, err := s.Read(ctx, attr(0, 0x001D, 0x0003), attr(1, 0x001D, 0x0003))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := []uint64{uint64(epA), uint64(epB)}
	slices.Sort(want)
	if got, _ := report.Value(0, 0x001D, 0x0003); !slices.Equal(uintList(got), append([]uint64{1}, want...)) {
		t.Errorf("root PartsList %v, want [1 %v]", uintList(got), want)
	}
	if got, _ := report.Value(1, 0x001D, 0x0003); !slices.Equal(uintList(got), want) {
		t.Errorf("Aggregator PartsList %v, want %v", uintList(got), want)
	}
	// The host drives reassembly itself, through the bridge.
	if err := n.Bridge().Reassemble(ctx); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	mu.Lock()
	if calls != 2 {
		t.Errorf("snapshotter called %d times after Reassemble, want 2", calls)
	}
	mu.Unlock()
}

// TestUniqueIDPinned: the first boot of a store derives UniqueID and pins
// it under store.SettingUniqueID; a later boot with another NodeLabel
// serves the pinned value, as matter.js serves the uniqueId it created
// once (BasicInformationServer.ts:89-91); a value flagged by the old
// rotation marker (store.SettingUniqueIDRotated) is migrated once; a
// host's own pin is served verbatim; a pin over 32 bytes is refused.
func TestUniqueIDPinned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	cfg := minimalConfig(db)
	readUniqueID := func(n *node.Node) string {
		v, ok := n.Root().BasicInformation.MatterRead(bidef.AttrUniqueId)
		if !ok {
			t.Fatal("UniqueID not served")
		}
		s, _ := v.(string)
		return s
	}
	n1 := newNode(t, cfg)
	first := readUniqueID(n1)
	pd, bi := cfg.ProductDescription, cfg.BasicInformation
	if want := mattercore.DeriveUniqueID(pd.VendorID, pd.ProductID, bi.NodeLabel, bi.SerialNumber); first != want {
		t.Errorf("first boot UniqueID %q, want the derived %q", first, want)
	}
	if stored, ok, err := n1.Store().GetSetting(ctx, store.SettingUniqueID); err != nil || !ok || stored != first {
		t.Errorf("stored %q (%v, %v), want %q", stored, ok, err, first)
	}
	if err := n1.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	cfg.BasicInformation.NodeLabel = "renamed node"
	n2 := newNode(t, cfg)
	if got := readUniqueID(n2); got != first {
		t.Errorf("after a label edit UniqueID %q, want the pinned %q", got, first)
	}
	if err := n2.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	// A value a host's own pinning flagged as written under rotation is
	// stale to a boot without it: re-derived, pinned, flag cleared — the
	// one-time migration until the deprecated key goes (issue #122).
	st := store.New(db)
	if err := st.SetSetting(ctx, store.SettingUniqueID, "rotated-per-boot"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, store.SettingUniqueIDRotated, "1"); err != nil { //nolint:staticcheck // the deprecated marker the migration reads
		t.Fatal(err)
	}
	n3 := newNode(t, cfg)
	want := mattercore.DeriveUniqueID(pd.VendorID, pd.ProductID, "renamed node", bi.SerialNumber)
	if got := readUniqueID(n3); got != want {
		t.Errorf("after a flagged value UniqueID %q, want re-derived %q", got, want)
	}
	if v, _, _ := st.GetSetting(ctx, store.SettingUniqueID); v != want {
		t.Errorf("pinned %q, want %q", v, want)
	}
	if flag, _, _ := st.GetSetting(ctx, store.SettingUniqueIDRotated); flag != "" { //nolint:staticcheck // as above
		t.Errorf("rotated flag %q still set", flag)
	}
	if err := n3.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	cfg.BasicInformation.UniqueID = "host-pinned-0123456789abcdef"
	n4 := newNode(t, cfg)
	if got := readUniqueID(n4); got != cfg.BasicInformation.UniqueID {
		t.Errorf("host pin served as %q", got)
	}

	cfg.BasicInformation.UniqueID = "123456789012345678901234567890123"
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Errorf("a 33-byte UniqueID passed Validate: %v", err)
	}
}

// TestLocationCapability: GeneralCommissioning LocationCapability is the
// configured value, Indoor when unset; an undefined enum value is refused.
func TestLocationCapability(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ set, want uint8 }{
		"default":        {0, mattercore.RegulatoryIndoor},
		"indoor outdoor": {mattercore.RegulatoryIndoorOutdoor, mattercore.RegulatoryIndoorOutdoor},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
			cfg.Commissioning.LocationCapability = tc.set
			n := newNode(t, cfg)
			v, ok := n.Root().GeneralCommissioning.MatterRead(gencommdef.AttrLocationCapability)
			if got, _ := v.(uint8); !ok || got != tc.want {
				t.Errorf("LocationCapability %v (%v), want %d", v, ok, tc.want)
			}
		})
	}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Commissioning.LocationCapability = mattercore.RegulatoryIndoorOutdoor + 1
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Errorf("LocationCapability 3 passed Validate: %v", err)
	}
}

// TestRemoveFabricFromHost: Node.RemoveFabric removes a commissioned
// fabric as the RemoveFabric command does — the store no longer lists it,
// the operational record is withdrawn, the node's own window reopens, the
// host's OnFabricRemoved observer runs with the fabric's identity — and an
// unknown index is store.ErrFabricNotFound.
func TestRemoveFabricFromHost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	var (
		mu       sync.Mutex
		removed  []node.FabricInfo
		recAtObs bool
	)
	cfg.Observers.OnFabricRemoved = func(info node.FabricInfo) error {
		mu.Lock()
		defer mu.Unlock()
		removed = append(removed, info)
		recAtObs = hasOperationalRecord(adv)
		return nil
	}
	n, c, _ := commissioned(t, cfg)
	if !hasOperationalRecord(adv) {
		t.Fatal("no operational record after commissioning")
	}
	if n.CommissioningWindow().IsOpen() {
		t.Fatal("the window is still open after commissioning")
	}

	if err := n.RemoveFabric(ctx, c.FabricIndex()); err != nil {
		t.Fatalf("RemoveFabric: %v", err)
	}
	fabrics, err := n.Fabrics(ctx)
	if err != nil || len(fabrics) != 0 {
		t.Errorf("fabrics after removal %v (%v), want none", fabrics, err)
	}
	if hasOperationalRecord(adv) {
		t.Error("the operational record is still published")
	}
	if _, mode, ok := commissionableRecord(adv); !ok || mode != 1 {
		t.Errorf("own window after the last fabric: record %v mode %d, want CM=1", ok, mode)
	}
	if !n.CommissioningWindow().IsOpen() {
		t.Error("the node's own window did not reopen")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(removed) != 1 {
		t.Fatalf("OnFabricRemoved ran %d times, want 1", len(removed))
	}
	if info := removed[0]; info.FabricIndex != c.FabricIndex() || info.FabricID != testFabricID || info.NodeID != testNodeID || info.CompressedFabricID != c.CompressedFabricID() {
		t.Errorf("OnFabricRemoved %+v, want index %d fabric 0x%X node 0x%X", info, c.FabricIndex(), testFabricID, testNodeID)
	}
	if recAtObs {
		t.Error("OnFabricRemoved ran before the facade withdrew the operational record")
	}

	if err := n.RemoveFabric(ctx, c.FabricIndex()); !errors.Is(err, store.ErrFabricNotFound) {
		t.Errorf("removing it again: %v, want store.ErrFabricNotFound", err)
	}
}

// TestSessionAndSubscriptionDiagnostics: Node.Sessions lists the PASE
// session a commissioner holds, then the CASE session of the fabric it
// commissioned, and Node.Subscriptions counts the subscription riding on
// that CASE session — a controller subscribes over CASE only, matter.js
// ClientInteraction.subscribe refusing a peer without a peer address
// (packages/protocol/src/action/client/ClientInteraction.ts:991-994 at
// 0ceb202d).
func TestSessionAndSubscriptionDiagnostics(t *testing.T) {
	// Not parallel: its second connection (PASE closed, then Commission over
	// a fresh discovery) lost the node under the package's parallel live
	// tests twice on CI on 2026-10-14 ("Could not connect to device").
	ctx := context.Background()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	// The node's debug log is kept and printed only when the test fails:
	// its second connection failed on CI with nothing at INFO to explain
	// why the bridge answered no PBKDFParamRequest.
	var nodeLog bytes.Buffer
	cfg.Logger = slog.New(slog.NewTextHandler(&nodeLog, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("node log:\n%s", nodeLog.String())
		}
	})
	n := newNode(t, cfg)
	if got := n.Sessions().Sessions(); len(got) != 0 {
		t.Errorf("sessions before any peer: %+v", got)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	pase, err := c.PASE(ctx)
	if err != nil {
		t.Fatalf("PASE: %v", err)
	}
	sessions := n.Sessions().Sessions()
	if len(sessions) != 1 || !sessions[0].IsPASE || sessions[0].FabricIndex != 0 {
		t.Fatalf("sessions after PASE %+v, want one PASE session on fabric 0", sessions)
	}
	pase.Close()
	s, err := c.Commission(ctx)
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	var operational []uint16
	for _, e := range n.Sessions().Sessions() {
		if !e.IsPASE && e.FabricIndex == c.FabricIndex() {
			operational = append(operational, e.SessionID)
		}
	}
	if len(operational) != 1 {
		t.Fatalf("sessions after commissioning %+v, want one CASE session on fabric %d", n.Sessions().Sessions(), c.FabricIndex())
	}
	if _, err := s.Subscribe(ctx, 0, 60, attr(0, 0x001D, 0x0003)); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if n.Subscriptions().Active() != 1 {
		t.Errorf("active subscriptions %d, want 1", n.Subscriptions().Active())
	}
	if got := n.Subscriptions().CountBySession(); got[operational[0]] != 1 {
		t.Errorf("subscriptions by session %v, want one on session %d", got, operational[0])
	}
}

// TestHostGroups: a host that assembles its own topology hands the facade
// the group manager its assembler mounts the Groups servers over
// (Config.Groups, issue #89); the facade runs on it and Node.Groups
// returns it. Without one the facade builds its own, which Node.Groups
// returns for a host binding its assembler after New.
func TestHostGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	gm, err := groups.NewManager(store.New(db), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	cfg := minimalConfig(db)
	cfg.Groups = gm
	n := newNode(t, cfg)
	if n.Groups() != gm {
		t.Fatal("the facade did not run on the host's group manager")
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	own := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "own.db"))))
	if own.Groups() == nil {
		t.Fatal("no group manager without Config.Groups")
	}
}
