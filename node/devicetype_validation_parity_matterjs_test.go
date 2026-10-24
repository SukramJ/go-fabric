// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The whole-node cases of matter.js
// packages/node/test/endpoint/validation/ConstructionValidationTest.ts and
// RuntimeValidationTest.ts at 862ad75f — the ones endpoint's ports
// (TestConstructionValidationParityMatterJS,
// TestRuntimeValidationParityMatterJS) skip with "needs a whole node": a
// strict node in its default configuration, an unknown mode, and a node in
// the default configuration that logs no device-type warning through its
// start, its runtime changes and its stop. They run on fabrictest devices
// with the module's kit devices; matter.js's "logged" is the device's
// matter.devicetype.violation records.
package node_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// violationsLogged is the matter.devicetype.violation records of l.
func violationsLogged(l *bqLog) []bqRecord {
	var out []bqRecord
	for _, r := range l.records() {
		if r.msg == "matter.devicetype.violation" {
			out = append(out, r)
		}
	}
	return out
}

// loggingSite is a site whose devices log into the returned log.
func loggingSite(t *testing.T) (*fabrictest.Site, *bqLog) {
	t.Helper()
	holdSite(t)
	lg := &bqLog{}
	return fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(&bqHandler{log: lg})}), lg
}

func kitLight(t *testing.T, key string) *device.OnOffLight {
	t.Helper()
	l, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey(key), Name: key})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestDeviceTypeValidationNodeParityMatterJS(t *testing.T) {
	ctx := parityCtx(t)

	t.Run("ConstructionValidationTest", func(t *testing.T) {
		t.Run("constructs a strict node in default configuration", func(t *testing.T) {
			site, lg := loggingSite(t)
			d := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
				cfg.DeviceTypeValidation = endpoint.DeviceTypeValidationStrict
			}})
			if d.Node() == nil {
				t.Fatal("the strict node did not start")
			}
			if len(lg.records()) == 0 {
				t.Fatal("the device logged nothing: the log is not wired")
			}
			if got := violationsLogged(lg); len(got) != 0 {
				t.Errorf("logged %v", got)
			}
		})

		t.Run("fails the node's construction for an unknown mode", func(t *testing.T) {
			cfg := minimalConfig(openDB(t, t.TempDir()+"/node.db"))
			cfg.DeviceTypeValidation = node.DeviceTypeValidationMode(7)
			if _, err := node.New(cfg); err == nil || !strings.Contains(err.Error(), "DeviceTypeValidation 7 is not a mode") {
				t.Fatalf("New = %v, want the unknown mode refused", err)
			}
		})

		t.Run("logs no warning for a plain light", func(t *testing.T) {
			site, lg := loggingSite(t)
			d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{kitLight(t, "light")}})
			if err := d.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if got := violationsLogged(lg); len(got) != 0 {
				t.Errorf("logged %v", got)
			}
		})

		t.Run("logs no warning for a bridge", func(t *testing.T) {
			site, lg := loggingSite(t)
			d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{kitLight(t, "light1"), kitLight(t, "light2")}})
			if err := d.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if got := violationsLogged(lg); len(got) != 0 {
				t.Errorf("logged %v", got)
			}
		})
	})

	t.Run("RuntimeValidationTest", func(t *testing.T) {
		t.Run("logs no warning for a light through start, runtime changes and close", func(t *testing.T) {
			site, lg := loggingSite(t)
			d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{kitLight(t, "light")}})
			sensor, err := device.NewTemperatureSensor(device.Options{StableKey: endpoint.StringKey("sensor"), Name: "sensor"})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Act(func(n *node.Node) error { return n.Aggregator().Add(context.Background(), sensor) }); err != nil {
				t.Fatal(err)
			}
			if err := d.Act(func(n *node.Node) error {
				return n.Aggregator().Remove(context.Background(), endpoint.StringKey("sensor"))
			}); err != nil {
				t.Fatal(err)
			}
			if err := d.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if got := violationsLogged(lg); len(got) != 0 {
				t.Errorf("logged %v", got)
			}
		})

		t.Run("logs no warning for a bridge through start, runtime changes and close", func(t *testing.T) {
			site, lg := loggingSite(t)
			d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{kitLight(t, "light1"), kitLight(t, "light2"), kitLight(t, "light3")}})
			for _, key := range []string{"light1", "light2"} {
				if err := d.Act(func(n *node.Node) error { return n.Aggregator().Remove(context.Background(), endpoint.StringKey(key)) }); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.Act(func(n *node.Node) error { return n.Aggregator().Add(context.Background(), kitLight(t, "light4")) }); err != nil {
				t.Fatal(err)
			}
			if err := d.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if got := violationsLogged(lg); len(got) != 0 {
				t.Errorf("logged %v", got)
			}
		})
	})
}
