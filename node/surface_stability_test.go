// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	closurecontroldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/surface"
)

// The settings the surface announcement reads and writes: the persisted
// surfaces and the ConfigurationVersions (node/persist.go,
// node/surface_stability.go).
var announcementKeys = []string{
	"bridged_surface_digest.", "basic_information.surface_digest",
	"bridged_configuration_version.", "basic_information.configuration_version",
}

// countingStore counts the writes to the announcement's settings.
type countingStore struct {
	store.Port
	mu     sync.Mutex
	writes []string
}

func (c *countingStore) SetSetting(ctx context.Context, key, value string) error {
	for _, p := range announcementKeys {
		if strings.HasPrefix(key, p) {
			c.mu.Lock()
			c.writes = append(c.writes, key)
			c.mu.Unlock()
			break
		}
	}
	return c.Port.SetSetting(ctx, key, value)
}

func (c *countingStore) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.writes
	c.writes = nil
	slices.Sort(out)
	return out
}

// stabilityRun is one run of a node over db: built under p with eps
// added, started; stop ends it.
type stabilityRun struct {
	n     *node.Node
	store *countingStore
}

func startRun(t *testing.T, db *sql.DB, p surface.Policies, configure func(*node.Config), eps ...node.Endpoint) stabilityRun {
	t.Helper()
	ctx := context.Background()
	counter := &countingStore{Port: store.New(db)}
	cfg := minimalConfig(db)
	cfg.Store.Credentials = counter
	cfg.Surface = p
	if configure != nil {
		configure(&cfg)
	}
	n := newNode(t, cfg)
	for _, ep := range eps {
		if err := n.Aggregator().Add(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return stabilityRun{n: n, store: counter}
}

func (r stabilityRun) stop(t *testing.T) {
	t.Helper()
	if err := r.n.Stop(context.Background()); err != nil && !errors.Is(err, node.ErrStopped) {
		t.Fatal(err)
	}
}

// rootVersion is the root's BasicInformation ConfigurationVersion.
func rootVersion(t *testing.T, n *node.Node) uint32 {
	t.Helper()
	v, ok := n.Root().BasicInformation.MatterRead(0x0018)
	if !ok {
		t.Fatal("BasicInformation serves no ConfigurationVersion")
	}
	return v.(uint32)
}

// bridged is the assembled endpoint with the stable key.
func bridged(t *testing.T, n *node.Node, key string) *endpoint.Endpoint {
	t.Helper()
	for _, ep := range n.Bridge().Topology().Bridged() {
		if ep.SourceKey != nil && ep.SourceKey.String() == key {
			return ep
		}
	}
	t.Fatalf("no bridged endpoint %q", key)
	return nil
}

// msch is the Thermostat MatterScheduleConfiguration feature, its register
// row the extension that enables it (thermo.Ahead).
var (
	msch    = thermostatdef.FeatureMatterScheduleConfiguration
	mschRow = thermo.Ahead.Row(surface.KindFeature, uint32(thermostatdef.FeatureMatterScheduleConfiguration))
)

// thermostat builds a heating thermostat with key, and MSCH with its
// schedule attributes when withSchedules is set.
func thermostat(t *testing.T, key string, withSchedules bool, p *surface.Policies) *device.Thermostat {
	t.Helper()
	decl := thermostatdef.Declare{Features: thermostatdef.FeatureHeating}
	if withSchedules {
		decl.Features |= msch
		types := []thermostatdef.ScheduleTypeStruct{{SystemMode: thermostatdef.SystemModeHeat, NumberOfSchedules: 1}}
		one, transitions := uint8(1), uint8(4)
		decl.Initial = thermostatdef.Patch{ScheduleTypes: &types, NumberOfSchedules: &one, NumberOfScheduleTransitions: &transitions}
	}
	d, err := device.NewThermostat(device.Options{StableKey: endpoint.StringKey(key), Name: key, Surface: p}, decl)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestSurfaceStableAcrossRestarts: the first assembly persists every
// endpoint's surface and raises nothing; a restart with the same
// composition writes none of the announcement's settings and announces
// nothing.
func TestSurfaceStableAcrossRestarts(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "stable.db"))
	first := startRun(t, db, surface.Policies{}, nil, light(t, "light", nil), thermostat(t, "thermo", false, nil))
	want := []string{
		"basic_information.surface_digest", "bridged_surface_digest.@1",
		"bridged_surface_digest.light", "bridged_surface_digest.thermo",
	}
	if got := first.store.take(); !slices.Equal(got, want) {
		t.Fatalf("first assembly wrote %v, want %v", got, want)
	}
	if rootVersion(t, first.n) != 1 || bridged(t, first.n, "thermo").ConfigurationVersion() != 1 {
		t.Fatal("the first assembly raised a ConfigurationVersion")
	}
	if rep := first.n.Surface(); len(rep.Changed) != 0 || !rep.Assembled {
		t.Fatalf("first assembly: changed %v, assembled %v", rep.Changed, rep.Assembled)
	}
	first.stop(t)

	again := startRun(t, db, surface.Policies{}, nil, light(t, "light", nil), thermostat(t, "thermo", false, nil))
	if got := again.store.take(); len(got) != 0 {
		t.Fatalf("a restart without a change wrote %v", got)
	}
	if rootVersion(t, again.n) != 1 || len(again.n.Surface().Changed) != 0 {
		t.Fatal("a restart without a change announced one")
	}
}

// TestSurfaceChangeAcrossARestart: enabling the MSCH extension on the
// thermostat between two runs raises its ConfigurationVersion and the
// root's by one, not the light's; the report names the change — the
// FeatureMap and the lists — and the versions persist, so a third run
// without a change writes nothing and serves them.
func TestSurfaceChangeAcrossARestart(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "change.db"))
	startRun(t, db, surface.Policies{}, nil, light(t, "light", nil), thermostat(t, "thermo", false, nil)).stop(t)

	p := surface.Policies{Extensions: surface.NewExtensions(mschRow)}
	run := startRun(t, db, p, nil, light(t, "light", &p), thermostat(t, "thermo", true, &p))
	if got := rootVersion(t, run.n); got != 2 {
		t.Errorf("root ConfigurationVersion %d, want 2", got)
	}
	if got := bridged(t, run.n, "thermo").ConfigurationVersion(); got != 2 {
		t.Errorf("thermostat ConfigurationVersion %d, want 2", got)
	}
	if got := bridged(t, run.n, "light").ConfigurationVersion(); got != 1 {
		t.Errorf("light ConfigurationVersion %d, want 1", got)
	}
	want := []string{"basic_information.configuration_version", "bridged_configuration_version.thermo", "bridged_surface_digest.thermo"}
	if got := run.store.take(); !slices.Equal(got, want) {
		t.Errorf("the change wrote %v, want %v", got, want)
	}
	rep := run.n.Surface()
	if len(rep.Changed) != 1 {
		t.Fatalf("changed %+v, want one entry", rep.Changed)
	}
	c := rep.Changed[0]
	if c.Endpoint != "thermo" || c.Before == c.After || c.At.IsZero() {
		t.Fatalf("change %+v", c)
	}
	kinds := map[surface.Kind]bool{}
	for _, ch := range c.Changes {
		if ch.Endpoint != "thermo" || ch.Cluster != thermostatdef.ClusterID {
			t.Errorf("a change outside the thermostat cluster: %s", ch)
		}
		kinds[ch.Kind] = true
		if ch.Kind == surface.KindFeature && (ch.ID != uint32(msch) || ch.After != "set") {
			t.Errorf("feature change %s, want MSCH set", ch)
		}
	}
	if !kinds[surface.KindFeature] || !kinds[surface.KindAttribute] || !kinds[surface.KindCommand] {
		t.Errorf("the change names %v, want the FeatureMap, the attributes and the commands", kinds)
	}
	for _, ep := range rep.Effective.Endpoints {
		if ep.Key == "thermo" && ep.Digest() != c.After {
			t.Error("the announced digest is not the served surface's")
		}
	}
	run.stop(t)

	third := startRun(t, db, p, nil, light(t, "light", &p), thermostat(t, "thermo", true, &p))
	if got := third.store.take(); len(got) != 0 {
		t.Errorf("a restart after the change wrote %v", got)
	}
	if rootVersion(t, third.n) != 2 || bridged(t, third.n, "thermo").ConfigurationVersion() != 2 {
		t.Error("the raised versions did not persist")
	}
}

// TestSurfaceChangeInProcess: a Replace that changes the surface raises
// the versions and bumps the DataVersion of the endpoint's Descriptor and
// of the changed cluster, not of a cluster that did not change.
func TestSurfaceChangeInProcess(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "replace.db"))
	p := surface.Policies{Extensions: surface.NewExtensions(mschRow)}
	run := startRun(t, db, p, nil, thermostat(t, "thermo", false, &p))
	ep := bridged(t, run.n, "thermo")
	const identify uint32 = 0x0003
	desc, therm, ident := ep.ClusterDataVersion(0x001D), ep.ClusterDataVersion(thermostatdef.ClusterID), ep.ClusterDataVersion(identify)
	if err := run.n.Aggregator().Replace(context.Background(), endpoint.StringKey("thermo"), thermostat(t, "thermo", true, &p)); err != nil {
		t.Fatal(err)
	}
	ep = bridged(t, run.n, "thermo")
	if ep.ClusterDataVersion(0x001D) == desc || ep.ClusterDataVersion(thermostatdef.ClusterID) == therm {
		t.Error("the Descriptor's or the Thermostat's DataVersion did not move")
	}
	if ep.ClusterDataVersion(identify) != ident {
		t.Error("Identify's DataVersion moved without a change")
	}
	if ep.ConfigurationVersion() != 2 || rootVersion(t, run.n) != 2 || len(run.n.Surface().Changed) != 1 {
		t.Errorf("versions %d, %d; changed %v", ep.ConfigurationVersion(), rootVersion(t, run.n), run.n.Surface().Changed)
	}
}

// TestSurfaceTwoChangesRaiseTheRootOnce: two endpoints changed in one
// assembly raise their own versions and the root's once.
func TestSurfaceTwoChangesRaiseTheRootOnce(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "two.db"))
	startRun(t, db, surface.Policies{}, nil, thermostat(t, "a", false, nil), thermostat(t, "b", false, nil)).stop(t)
	p := surface.Policies{Extensions: surface.NewExtensions(mschRow)}
	run := startRun(t, db, p, nil, thermostat(t, "a", true, &p), thermostat(t, "b", true, &p))
	if got := rootVersion(t, run.n); got != 2 {
		t.Errorf("root ConfigurationVersion %d, want 2 (one raise for both)", got)
	}
	for _, key := range []string{"a", "b"} {
		if got := bridged(t, run.n, key).ConfigurationVersion(); got != 2 {
			t.Errorf("%s ConfigurationVersion %d, want 2", key, got)
		}
	}
	if got := len(run.n.Surface().Changed); got != 2 {
		t.Errorf("%d changes, want 2", got)
	}
}

// TestSurfaceRootChangeRaisesTheRootAlone: a node configuration that
// changes the root's surface (TimeSynchronization's features) raises the
// root's version and no bridged endpoint's.
func TestSurfaceRootChangeRaisesTheRootAlone(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "root.db"))
	startRun(t, db, surface.Policies{}, nil, light(t, "light", nil)).stop(t)
	p := surface.Policies{Extensions: surface.NewExtensions(timeSyncRow)}
	run := startRun(t, db, p, func(c *node.Config) {
		c.TimeSynchronization = &node.TimeSynchronizationConfig{Features: timeSyncFeatures}
	}, light(t, "light", &p))
	if got := rootVersion(t, run.n); got != 2 {
		t.Errorf("root ConfigurationVersion %d, want 2", got)
	}
	if got := bridged(t, run.n, "light").ConfigurationVersion(); got != 1 {
		t.Errorf("light ConfigurationVersion %d, want 1", got)
	}
	changed := run.n.Surface().Changed
	if len(changed) != 1 || changed[0].Endpoint != "" {
		t.Fatalf("changes %+v, want the root's alone", changed)
	}
	want := []string{"basic_information.configuration_version", "basic_information.surface_digest"}
	if got := run.store.take(); !slices.Equal(got, want) {
		t.Errorf("the root change wrote %v, want %v", got, want)
	}
}

// TestSurfaceWithheldIsAFirstAssembly: an endpoint the specification
// level withholds serves nothing, so the surface kept for it is dropped;
// served again it is a first assembly — its surface written anew, nothing
// raised (without the drop, the same surface would have written nothing).
func TestSurfaceWithheldIsAFirstAssembly(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "withheld.db"))
	closure := func(p *surface.Policies, label string) node.Endpoint {
		d, err := device.NewClosure(device.Options{StableKey: endpoint.StringKey("closure"), Name: label, Surface: p},
			closurecontroldef.Declare{Features: closurecontroldef.FeaturePositioning})
		if d == nil {
			t.Fatalf("closure: %v", err)
		}
		return d
	}
	startRun(t, db, surface.Policies{}, nil, light(t, "light", nil), closure(nil, "closure")).stop(t)

	low := surface.Policies{Level: "1.4.2"}
	ctx := context.Background()
	counter := &countingStore{Port: store.New(db)}
	cfg := minimalConfig(db)
	cfg.Store.Credentials, cfg.Surface = counter, low
	n := newNode(t, cfg)
	if err := n.Aggregator().Add(ctx, light(t, "light", &low)); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, closure(&low, "closure")); !errors.Is(err, surface.ErrWithheld) {
		t.Fatalf("the closure at 1.4.2: %v, want withheld", err)
	}
	if got := counter.take(); !slices.Equal(got, []string{"bridged_surface_digest.closure"}) {
		t.Errorf("withholding wrote %v, want the closure's surface dropped", got)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if v := rootVersion(t, n); v != 1 || len(n.Surface().Changed) != 0 {
		t.Errorf("withholding announced a change: root %d, %v", v, n.Surface().Changed)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	back := startRun(t, db, surface.Policies{}, nil, light(t, "light", nil), closure(nil, "closure"))
	if got := back.store.take(); !slices.Equal(got, []string{"bridged_surface_digest.closure"}) {
		t.Errorf("the return wrote %v, want the closure's surface alone", got)
	}
	if rootVersion(t, back.n) != 1 || len(back.n.Surface().Changed) != 0 {
		t.Error("the return of a withheld endpoint was announced as a change")
	}
}

// TestSurfaceBridgedConfigurationVersionAtAnOlderLevel is concept v11
// D.9 at work: at 1.4.1, which does not know ConfigurationVersion, the
// assembled report shows BridgedDeviceBasicInformation serving it on a
// bridged endpoint, kept by the level with the surface-change reason; at
// the pin there is no such decision (the control).
func TestSurfaceBridgedConfigurationVersionAtAnOlderLevel(t *testing.T) {
	const bdbi, configurationVersion = mattercore.BridgedDeviceBasicInformationClusterID, uint32(0x0018)
	for _, level := range []surface.Level{"1.4.1", ""} {
		p := surface.Policies{Level: level}
		run := startRun(t, openDB(t, filepath.Join(t.TempDir(), "d9.db")), p, nil, light(t, "light", &p))
		rep := run.n.Surface()
		served := false
		for _, ep := range rep.Effective.Endpoints {
			for _, c := range ep.Clusters {
				if ep.Key == "light" && c.ID == bdbi && slices.Contains(c.Attributes, configurationVersion) {
					served = true
				}
			}
		}
		if !served {
			t.Fatalf("level %q: the light serves no ConfigurationVersion", level)
		}
		kept := false
		for _, d := range rep.Decisions {
			if d.Endpoint == "light" && d.Cluster == bdbi && d.Kind == surface.KindAttribute && d.ID == configurationVersion {
				kept = d.Policy == "level" && d.Verdict == surface.Keep && d.Reason == surface.ReasonSurfaceChangeSignal
			}
		}
		if want := level != ""; kept != want {
			t.Errorf("level %q: the D.9 keep decision present %v, want %v", level, kept, want)
		}
	}
}
