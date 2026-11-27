// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	thermostatdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/surface"
)

// TestDumpSurface: --dump-surface composes the fleet without touching
// --db or its OTA directory, and writes a report that parses, whose
// compact form is the canonical encoding, with the root, the Aggregator
// and the fleet.
func TestDumpSurface(t *testing.T) {
	dir := t.TempDir()
	f := flags{dbPath: filepath.Join(dir, "daemon.db"), listen: "127.0.0.1:0", label: "surface-test", salt: "go-fabric-ref-salt", iterations: 1000, passcode: 20202021, discriminator: 3840, advertise: true}
	var out bytes.Buffer
	if err := dumpSurface(context.Background(), f, slog.New(slog.DiscardHandler), 20202021, 3840, node.TCPConfig{}, &out); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{f.dbPath, filepath.Join(dir, "ota")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("--dump-surface created %s", p)
		}
	}
	var rep surface.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, out.Bytes()); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compact.Bytes(), canonical) {
		t.Fatal("the dump is not the canonical encoding laid out")
	}
	if rep.Policies != (surface.Policies{}).Digest() || len(rep.Effective.Root.Clusters) == 0 {
		t.Fatalf("report %x root %+v", rep.Policies, rep.Effective.Root)
	}
	keys := map[string]bool{}
	for _, ep := range rep.Effective.Endpoints {
		keys[ep.Key] = true
	}
	if !keys[node.AggregatorSurfaceKey] || !keys["demo:light:1"] || len(keys) < 30 {
		t.Fatalf("endpoints %v", keys)
	}
	// One line per cluster and per decision.
	if !strings.Contains(out.String(), "\n    {\"cluster\":") || !strings.Contains(out.String(), "\n          {\"accepted\":") {
		t.Fatalf("layout:\n%.600s", out.String())
	}
}

// thermostatFeatureMap returns the demo thermostat's FeatureMap in a
// --dump-surface report.
func thermostatFeatureMap(t *testing.T, rep surface.Report) uint32 {
	t.Helper()
	for _, ep := range rep.Effective.Endpoints {
		if ep.Key != "demo:thermostat:1" {
			continue
		}
		for _, c := range ep.Clusters {
			if c.ID == thermostatdef.ClusterID {
				return c.FeatureMap
			}
		}
	}
	t.Fatal("no demo thermostat in the report")
	return 0
}

// dumpReport is the daemon's --dump-surface report under f's flags.
func dumpReport(t *testing.T, f flags) surface.Report {
	t.Helper()
	dir := t.TempDir()
	f.dbPath, f.listen, f.label, f.salt = filepath.Join(dir, "daemon.db"), "127.0.0.1:0", "surface-test", "go-fabric-ref-salt"
	f.iterations, f.passcode, f.discriminator, f.advertise, f.media = 1000, 20202021, 3840, true, true
	var out bytes.Buffer
	if err := dumpSurface(context.Background(), f, slog.New(slog.DiscardHandler), 20202021, 3840, node.TCPConfig{}, &out); err != nil {
		t.Fatal(err)
	}
	var rep surface.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

// TestDumpSurfaceWithThermostatSchedules: --thermostat-schedules is the
// demo thermostat's own switch (owner decision 732) — it serves MSCH under
// the zero-value policies, no decision names a register row and the report
// lists no extension; the retired extension BD-chip-Thermostat-Schedules
// in --extensions selects nothing (owner decisions 731, 740).
func TestDumpSurfaceWithThermostatSchedules(t *testing.T) {
	msch := uint32(thermostatdef.FeatureMatterScheduleConfiguration)
	rep := dumpReport(t, flags{thermostatSchedules: true})
	if rep.Policies != (surface.Policies{}).Digest() || rep.Extensions != nil {
		t.Errorf("policies %x, extensions %v; want the zero value's", rep.Policies, rep.Extensions)
	}
	for _, d := range rep.Decisions {
		if d.Reference.Ahead != "" {
			t.Errorf("decision %+v names a row", d)
		}
	}
	if fm := thermostatFeatureMap(t, rep); fm&msch == 0 {
		t.Errorf("FeatureMap 0x%X lacks MSCH with --thermostat-schedules", fm)
	}
	retired := dumpReport(t, flags{extensions: "BD-chip-Thermostat-Schedules"})
	if retired.Policies != (surface.Policies{}).Digest() || retired.Extensions != nil {
		t.Errorf("the retired extension reaches the report: %x, %v", retired.Policies, retired.Extensions)
	}
	if fm := thermostatFeatureMap(t, retired); fm&msch != 0 {
		t.Errorf("FeatureMap 0x%X serves MSCH for the retired extension", fm)
	}
}

func TestSurfacePolicies(t *testing.T) {
	p, err := surfacePolicies(flags{})
	if err != nil || p.Digest() != (surface.Policies{}).Digest() {
		t.Fatalf("no flag: %+v, %v; want the zero value", p, err)
	}
	p, err = surfacePolicies(flags{extensions: " BD-chip-Thermostat-Events, ,BD-chip-TimeSynchronization-Features", thermostatSchedules: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Extensions.IDs(); strings.Join(got, ",") != "BD-chip-Thermostat-Events,BD-chip-TimeSynchronization-Features" {
		t.Errorf("extensions %v, want --thermostat-schedules to add none", got)
	}
	// The retired ID is accepted and selects nothing (owner decision 731).
	p, err = surfacePolicies(flags{extensions: "BD-chip-Thermostat-Schedules"})
	if err != nil || p.Digest() != (surface.Policies{}).Digest() {
		t.Fatalf("the retired ID: %v, digest %x; want accepted, the zero value's", err, p.Digest())
	}
	_, err = surfacePolicies(flags{extensions: "BD-chip-Thermostat-Schedule"})
	if err == nil || !strings.Contains(err.Error(), `"BD-chip-Thermostat-Schedule"`) || !strings.Contains(err.Error(), "BD-Thread-NetComm-ThreadVariant") {
		t.Fatalf("a typo: %v, want it named with the known IDs", err)
	}
}

func TestFormatSurface(t *testing.T) {
	in := `{"a":[],"b":{"c":[1,2],"d":"x y"},"e":[{"f":[[1],[2]]},3]}`
	got, err := formatSurface([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": [],\n  \"b\": {\"c\":[1,2],\"d\":\"x y\"},\n  \"e\": [\n    {\n      \"f\": [\n        [1],\n        [2]\n      ]\n    },\n    3\n  ]\n}\n"
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if _, err := formatSurface([]byte(`{"a":`)); err == nil {
		t.Fatal("malformed JSON formatted")
	}
}
