// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

// featureDecisions are the decisions on features.
func featureDecisions(ds []surface.Decision) []surface.Decision {
	var out []surface.Decision
	for _, d := range ds {
		if d.Kind == surface.KindFeature {
			out = append(out, d)
		}
	}
	return out
}

// groupcastID is the Groupcast cluster (0x0065): a root cluster the levels
// below 1.6.0 do not know (parity/levels, measured).
const groupcastID uint32 = 0x0065

func TestNodeRejectsAnUnknownLevel(t *testing.T) {
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "level.db")))
	cfg.Surface = surface.Policies{Level: "1.5"}
	if _, err := node.New(cfg); !errors.Is(err, surface.ErrUnknownLevel) {
		t.Fatalf("node.New at level 1.5: %v", err)
	}
}

// TestNodeRootAtALevel: at 1.4.2 the root keeps Groupcast, which the pin
// may mandate, its decision naming the level; at
// the pin (and with the zero value) the root is unchanged and no level
// decision exists.
func TestNodeRootAtALevel(t *testing.T) {
	pin := surfaceNode(t, surface.Policies{}).Surface()
	explicit := surfaceNode(t, surface.Policies{Level: surface.PinLevel()}).Surface()
	if pin.Effective.Root.Digest() != explicit.Effective.Root.Digest() || pin.Level != surface.PinLevel() {
		t.Fatal("the pin's level composes the root like the zero value")
	}
	for _, d := range append(pin.Decisions, explicit.Decisions...) {
		if d.Policy == "level" {
			t.Errorf("a level decision at the pin: %+v", d)
		}
	}
	if rootCluster(pin, groupcastID).ID != groupcastID {
		t.Fatal("the pin's root serves Groupcast")
	}
	low := surfaceNode(t, surface.Policies{Level: "1.4.2"}).Surface()
	if low.Level != "1.4.2" {
		t.Errorf("report level %q", low.Level)
	}
	// Groupcast's RootNode conformance may make it mandatory at the pin
	// (rootMandatory): kept, the reason naming the level.
	if rootCluster(low, groupcastID).ID != groupcastID {
		t.Error("Groupcast is not mounted at 1.4.2")
	}
	var found bool
	for _, d := range low.Decisions {
		if d.Kind == surface.KindCluster && d.Endpoint == "" && d.Policy == "level" {
			if d.ID != groupcastID || d.Verdict != surface.Keep || d.Reason != "kept: mandatory at pin (absent at level 1.4.2)" || !d.Mandatory {
				t.Errorf("root cluster decision %+v", d)
			}
			found = true
		}
	}
	if !found {
		t.Error("no level decision on Groupcast")
	}
	// A root cluster a policy removes is not mounted: FixedLabel, a kit
	// default, by a custom policy.
	const fixedLabel = 0x0040
	cut := surfaceNode(t, surface.Policies{Custom: []surface.Policy{surfacetest.Remove(surface.KindCluster, surfacetest.Match{ID: fixedLabel})}}).Surface()
	if rootCluster(cut, fixedLabel).ID != 0 || len(cut.Effective.Root.Clusters) != len(pin.Effective.Root.Clusters)-1 {
		t.Errorf("FixedLabel removed: %d root clusters, %d at the pin", len(cut.Effective.Root.Clusters), len(pin.Effective.Root.Clusters))
	}
}

// TestAddRecordsAWithheldDevice: a Closure (0x0230) is not in the 1.4.2
// model (parity/levels, measured): device.New returns it with a
// WithheldError, Add returns the same error and the node reports it as
// withheld; a device of the same key that is not withheld clears it.
func TestAddRecordsAWithheldDevice(t *testing.T) {
	ctx := context.Background()
	p := surface.Policies{Level: "1.4.2"}
	n := surfaceNode(t, p)
	d, err := device.NewClosure(device.Options{StableKey: endpoint.StringKey("garage"), Name: "garage", Surface: &p})
	var w *surface.WithheldError
	if !errors.As(err, &w) || d == nil {
		t.Fatalf("NewClosure at 1.4.2: %v, %v", d, err)
	}
	if w.Endpoint != "garage" || len(w.Elements) == 0 || w.Elements[0].Kind != surface.KindDeviceType || w.Report.Level != "1.4.2" {
		t.Fatalf("withheld %+v", w)
	}
	if rep := d.Surface(); len(rep.Withheld) == 0 || len(rep.Effective.Endpoints) != 0 || len(rep.Decisions) == 0 {
		t.Fatalf("withheld device report %+v", rep)
	}
	if err := n.Aggregator().Add(ctx, d); !errors.Is(err, surface.ErrWithheld) {
		t.Fatalf("Add of a withheld device: %v", err)
	}
	rep := n.Surface()
	if len(rep.Withheld) != len(w.Elements) || rep.Withheld[0].Endpoint != "garage" {
		t.Fatalf("node withheld %+v", rep.Withheld)
	}
	for _, ep := range rep.Effective.Endpoints {
		if ep.Key == "garage" {
			t.Fatal("a withheld device is in the effective surface")
		}
	}
	// The same key, a light at the same level: added, the record gone.
	if err := n.Aggregator().Add(ctx, light(t, "garage", &p)); err != nil {
		t.Fatal(err)
	}
	if rep := n.Surface(); len(rep.Withheld) != 0 {
		t.Fatalf("withheld after a replacement add: %+v", rep.Withheld)
	}
}

func TestNodeServerInternalAtALevel(t *testing.T) {
	rep := surfaceNode(t, surface.Policies{Level: "1.4"}).Surface()
	for _, d := range rep.Decisions {
		if d.Reason != surface.ReasonServerInternal {
			continue
		}
		if d.Policy != "level" || d.Verdict != surface.Keep || (d.Endpoint != "" && d.Endpoint != node.AggregatorSurfaceKey) {
			t.Errorf("server-internal decision %+v", d)
		}
		if d.Levels.Has("1.4") {
			t.Errorf("%s is known at 1.4", d.Element)
		}
	}
}
