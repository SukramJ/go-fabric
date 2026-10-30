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

	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
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

// TestDumpSurfaceWithExtensions: --thermostat-schedules composes the
// whole fleet under the policies with the extension — every device carries
// them, or the node would refuse it — and the thermostat's MSCH decision
// names its register row.
func TestDumpSurfaceWithExtensions(t *testing.T) {
	dir := t.TempDir()
	f := flags{
		dbPath: filepath.Join(dir, "daemon.db"), listen: "127.0.0.1:0", label: "surface-test", salt: "go-fabric-ref-salt",
		iterations: 1000, passcode: 20202021, discriminator: 3840, advertise: true, thermostatSchedules: true, media: true,
	}
	var out bytes.Buffer
	if err := dumpSurface(context.Background(), f, slog.New(slog.DiscardHandler), 20202021, 3840, node.TCPConfig{}, &out); err != nil {
		t.Fatal(err)
	}
	var rep surface.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	want := surface.Policies{Extensions: surface.NewExtensions(schedulesRow)}
	if rep.Policies != want.Digest() {
		t.Fatalf("policies %x, want the extension's", rep.Policies)
	}
	tagged := 0
	for _, d := range rep.Decisions {
		if d.Reference.Ahead != "" {
			tagged++
			if d.Reference.Ahead != schedulesRow || d.Endpoint != "demo:thermostat:1" || d.Kind != surface.KindFeature {
				t.Errorf("decision %+v", d)
			}
		}
	}
	if tagged != 1 || !strings.Contains(out.String(), `"reference":"`+schedulesRow+`"`) {
		t.Fatalf("%d decisions name a row, want the thermostat's MSCH", tagged)
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
	if got := p.Extensions.IDs(); strings.Join(got, ",") != "BD-chip-Thermostat-Events,BD-chip-Thermostat-Schedules,BD-chip-TimeSynchronization-Features" {
		t.Errorf("extensions %v", got)
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
