// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The surface column of the register (concept v11 C.4, ADR 0030): which
// rows change what a controller sees, measured by building each declared
// cluster with and without the row's elements, and held against the
// servers' declarations in both directions.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/cluster/core"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	tbrmdef "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/thread"
)

// declaredRows are the register IDs the servers declare: the device
// layer's and the node's, sorted and unique.
func declaredRows() []string {
	out := slices.Concat(device.AheadRows(), node.AheadRows())
	slices.Sort(out)
	return slices.Compact(out)
}

// completeness holds the register's surface column against the declared
// register IDs: a "yes" row must be declared, a declared ID must be a
// "yes" row, a "no" row must not be declared, and every row must carry a
// value. One finding per offence, sorted.
func completeness(rows []row, declared []string) []string {
	var out []string
	inRegister := map[string]bool{}
	for _, r := range rows {
		inRegister[r.entry] = true
		isDeclared := slices.Contains(declared, r.entry)
		switch {
		case r.surface == "":
			out = append(out, fmt.Sprintf("%s (by_design.md:%d): no surface value", r.entry, r.line))
		case r.surface == "yes" && !isDeclared:
			out = append(out, fmt.Sprintf("%s (by_design.md:%d): surface yes, but no server declares it (device.AheadRows, node.AheadRows)", r.entry, r.line))
		case r.surface == "no" && isDeclared:
			out = append(out, fmt.Sprintf("%s (by_design.md:%d): a server declares it, but surface is no", r.entry, r.line))
		}
	}
	for _, id := range declared {
		if !inRegister[id] {
			out = append(out, id+": declared by a server, but no row of the register")
		}
	}
	slices.Sort(out)
	return out
}

func committedRegister(t *testing.T) []row {
	t.Helper()
	doc, err := os.ReadFile(filepath.Join("..", "..", registerPath))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseRegister(string(doc))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestSurfaceColumnMatchesTheDeclarations is the two-way completeness
// check of the surface column.
func TestSurfaceColumnMatchesTheDeclarations(t *testing.T) {
	rows := committedRegister(t)
	if f := completeness(rows, declaredRows()); len(f) > 0 {
		t.Errorf("the surface column against the declarations:\n%s", strings.Join(f, "\n"))
	}
}

// TestSurfaceCompletenessBites: a declaration deleted, and one invented,
// are each reported, and nothing else.
func TestSurfaceCompletenessBites(t *testing.T) {
	rows := committedRegister(t)
	declared := declaredRows()
	if len(declared) == 0 {
		t.Fatal("no declaration")
	}
	for i, id := range declared {
		fewer := slices.Delete(slices.Clone(declared), i, i+1)
		f := completeness(rows, fewer)
		if len(f) != 1 || !strings.HasPrefix(f[0], id+" ") || !strings.Contains(f[0], "no server declares it") {
			t.Errorf("without %s: findings %q, want that row alone", id, f)
		}
	}
	const invented = "BD-Invented-Row"
	f := completeness(rows, append(slices.Clone(declared), invented))
	if len(f) != 1 || !strings.HasPrefix(f[0], invented+":") {
		t.Errorf("with %s: findings %q, want that ID alone", invented, f)
	}
	// A "no" row a server declares is reported too.
	var no string
	for _, r := range rows {
		if r.surface == "no" {
			no = r.entry
			break
		}
	}
	if f := completeness(rows, append(slices.Clone(declared), no)); len(f) != 1 || !strings.Contains(f[0], "surface is no") {
		t.Errorf("with the no row %s declared: findings %q", no, f)
	}
	// A register without the column has no value anywhere.
	bare := slices.Clone(rows[:1])
	bare[0].surface = ""
	if f := completeness(bare, nil); len(f) != 1 || !strings.Contains(f[0], "no surface value") {
		t.Errorf("a row without a value: findings %q", f)
	}
}

func TestParseRegisterSurfaceColumn(t *testing.T) {
	head := registerHeading + "\n\n| Entry | marker file | marker text | surface |\n| --- | --- | --- | --- |\n"
	rows, err := parseRegister(head + "| A | `a.ts` | `\"x\"` | yes |\n| B | `b.ts` | `\"y\"` | no |\n| C | `c.ts` | `\"z\"` | maybe |\n| D | `d.ts` | `\"w\"` |\n")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].surface != "yes" || rows[1].surface != "no" || rows[0].problem != "" || rows[1].problem != "" {
		t.Errorf("rows %+v", rows[:2])
	}
	if !strings.Contains(rows[2].problem, `surface is "maybe"`) || !strings.Contains(rows[3].problem, `surface is ""`) {
		t.Errorf("rows %+v, want the bad and the missing value reported", rows[2:])
	}
}

// measure builds one declared row's cluster without (false) and with
// (true) the row's elements and returns its surface.
type measure func(t *testing.T, row string, with bool) surface.Cluster

// measurements name how each declared row is measured.
var measurements = map[string]measure{
	"BD-chip-Thermostat-Schedules":         measureThermostat,
	"BD-chip-Thermostat-Events":            measureThermostat,
	"BD-chip-TimeSynchronization-Features": measureTimeSynchronization,
	"BD-Thread-NetComm-ThreadVariant":      measureNetworkCommissioning,
}

// measureThermostat builds a heating Thermostat through the device layer
// with every Thermostat row enabled, with or without the row's features;
// MatterScheduleConfiguration carries the one schedule type the server
// needs to start.
func measureThermostat(t *testing.T, row string, with bool) surface.Cluster {
	t.Helper()
	features := thermostatdef.FeatureHeating
	decl := thermostatdef.Declare{}
	if with {
		features |= thermostatdef.Feature(thermo.Ahead[row].Features)
	}
	if features&thermostatdef.FeatureMatterScheduleConfiguration != 0 {
		types := []thermostatdef.ScheduleTypeStruct{{SystemMode: thermostatdef.SystemModeHeat, NumberOfSchedules: 1}}
		one, transitions := uint8(1), uint8(4)
		decl.Initial = thermostatdef.Patch{ScheduleTypes: &types, NumberOfSchedules: &one, NumberOfScheduleTransitions: &transitions}
	}
	decl.Features = features
	p := surface.Policies{Extensions: surface.NewExtensions(thermo.Ahead.IDs()...)}
	d, err := device.NewThermostat(device.Options{StableKey: endpoint.StringKey("t"), Name: "t", Surface: &p}, decl)
	if err != nil {
		t.Fatalf("%s: %v", row, err)
	}
	return endpoint.ClusterSurface(d.Server(thermostatdef.ClusterID))
}

var memoryDBs atomic.Int64

// measureTimeSynchronization builds a node on an in-memory store, as
// --dump-surface does, with the row enabled and the root's
// TimeSynchronization with or without the row's features.
func measureTimeSynchronization(t *testing.T, row string, with bool) surface.Cluster {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:aheadcheck-%d?mode=memory&cache=shared", memoryDBs.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := node.Config{
		Commissioning: node.Commissioning{Passcode: 20202021, Discriminator: 3840, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{
			VendorID: 0xFFF1, ProductID: 0x8001,
		},
		BasicInformation: node.BasicInformation{
			VendorName: "aheadcheck", ProductName: "aheadcheck", NodeLabel: "aheadcheck", SerialNumber: "AHEAD-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:   node.Store{DB: db},
		Logger:  slog.New(slog.DiscardHandler),
		Network: node.Network{Listen: "127.0.0.1:0", PreferIPv4: true, MDNS: node.MDNS{Disabled: true}},
		Surface: surface.Policies{Extensions: surface.NewExtensions(row)},
	}
	if with {
		cfg.TimeSynchronization = &node.TimeSynchronizationConfig{Features: tsdef.Feature(core.TimeSynchronizationAhead[row].Features)}
	}
	n, err := node.New(cfg)
	if err != nil {
		t.Fatalf("%s: %v", row, err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	for _, c := range n.Surface().Effective.Root.Clusters {
		if c.ID == tsdef.ClusterID {
			return c
		}
	}
	t.Fatalf("%s: the root serves no TimeSynchronization", row)
	return surface.Cluster{}
}

// measureNetworkCommissioning builds the root's NetworkCommissioning
// without the row — the Ethernet server node.New mounts — and with it:
// the Thread variant, over a border router that answers nothing.
func measureNetworkCommissioning(t *testing.T, row string, with bool) surface.Cluster {
	t.Helper()
	if !with {
		return endpoint.ClusterSurface(core.NewNetworkCommissioning(core.NetworkCommissioningConfig{}))
	}
	srv, err := core.NewThreadNetworkCommissioning(context.Background(), core.ThreadNetworkCommissioningConfig{BorderRouter: silentBorderRouter{}})
	if err != nil {
		t.Fatalf("%s: %v", row, err)
	}
	c := endpoint.ClusterSurface(srv)
	if c.FeatureMap&core.NetworkCommissioningAhead[row].Features == 0 {
		t.Fatalf("%s: the Thread variant's FeatureMap 0x%X lacks the declared bit", row, c.FeatureMap)
	}
	return c
}

// silentBorderRouter is a border router that answers every call with an
// error: the Thread variant then starts with no dataset.
type silentBorderRouter struct{}

var errSilent = errors.New("aheadcheck: no border router")

func (silentBorderRouter) BorderRouterName() string                       { return "aheadcheck" }
func (silentBorderRouter) BorderAgentID(context.Context) ([]byte, error)  { return nil, errSilent }
func (silentBorderRouter) ThreadVersion(context.Context) (uint16, error)  { return 0, errSilent }
func (silentBorderRouter) InterfaceEnabled(context.Context) (bool, error) { return false, errSilent }

func (silentBorderRouter) Dataset(context.Context, bool) ([]byte, error)   { return nil, errSilent }
func (silentBorderRouter) SetActiveDataset(context.Context, []byte) error  { return errSilent }
func (silentBorderRouter) CommitActiveDataset(context.Context) error       { return errSilent }
func (silentBorderRouter) RevertActiveDataset(context.Context) error       { return errSilent }
func (silentBorderRouter) SetPendingDataset(context.Context, []byte) error { return errSilent }
func (silentBorderRouter) Diagnostics(context.Context) (thread.NodeDiagnostics, error) {
	return thread.NodeDiagnostics{}, errSilent
}

var _ tbrmdef.BorderRouter = silentBorderRouter{}

// differingLists names what differs between two builds of a cluster:
// FeatureMap and the four lists.
func differingLists(a, b surface.Cluster) []string {
	var out []string
	if a.FeatureMap != b.FeatureMap {
		out = append(out, "FeatureMap")
	}
	for _, l := range []struct {
		name string
		x, y []uint32
	}{
		{"AttributeList", a.Attributes, b.Attributes},
		{"AcceptedCommandList", a.Accepted, b.Accepted},
		{"GeneratedCommandList", a.Generated, b.Generated},
		{"EventList", a.Events, b.Events},
	} {
		if !reflect.DeepEqual(slices.Sorted(slices.Values(l.x)), slices.Sorted(slices.Values(l.y))) {
			out = append(out, l.name)
		}
	}
	return out
}

// TestSurfaceColumnMeasured measures every declared row: it is
// surface-changing when FeatureMap or one of the four lists differs
// between the cluster built with and without the row's elements. Every
// declared row must measure yes, and the register must say so. -v logs
// the measurement table.
func TestSurfaceColumnMeasured(t *testing.T) {
	rows := committedRegister(t)
	column := map[string]string{}
	for _, r := range rows {
		column[r.entry] = r.surface
	}
	for _, id := range declaredRows() {
		m, ok := measurements[id]
		if !ok {
			t.Errorf("%s: declared, but the test does not know how to measure it — add it to measurements", id)
			continue
		}
		diff := differingLists(m(t, id, false), m(t, id, true))
		measured := "no"
		if len(diff) > 0 {
			measured = "yes"
		}
		t.Logf("| %s | %s | %s |", id, measured, strings.Join(diff, ", "))
		if measured != "yes" {
			t.Errorf("%s: measures no — a declared row must change the surface", id)
		}
		if column[id] != measured {
			t.Errorf("%s: the register says surface %q, the measurement %q", id, column[id], measured)
		}
	}
	for id := range measurements {
		if !slices.Contains(declaredRows(), id) {
			t.Errorf("%s: measured, but no server declares it", id)
		}
	}
}
