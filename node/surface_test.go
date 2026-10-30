// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

func surfaceNode(t *testing.T, p surface.Policies) *node.Node {
	t.Helper()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "surface.db")))
	cfg.Surface = p
	return newNode(t, cfg)
}

func light(t *testing.T, key string, p *surface.Policies) *device.OnOffLight {
	t.Helper()
	d, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey(key), Name: key, Surface: p})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAddRefusesOtherSurfacePolicies(t *testing.T) {
	ctx := context.Background()
	keepAll := surface.Policies{Custom: []surface.Policy{surfacetest.KeepAll()}}

	n := surfaceNode(t, surface.Policies{})
	agg := n.Aggregator()
	err := agg.Add(ctx, light(t, "foreign", &keepAll))
	if !errors.Is(err, node.ErrPolicyMismatch) || !strings.Contains(err.Error(), `"foreign"`) {
		t.Fatalf("Add of a device under other policies: %v", err)
	}
	// nil and the zero value are the same policies.
	if err := agg.Add(ctx, light(t, "nil", nil)); err != nil {
		t.Fatal(err)
	}
	if err := agg.Add(ctx, light(t, "zero", &surface.Policies{})); err != nil {
		t.Fatal(err)
	}
	// Replace checks as Add does.
	if err := agg.Replace(ctx, endpoint.StringKey("nil"), light(t, "nil", &keepAll)); !errors.Is(err, node.ErrPolicyMismatch) {
		t.Fatalf("Replace with a device under other policies: %v", err)
	}
	// A part is checked like its owner.
	owner, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("owner")})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AddPart(light(t, "part", &keepAll).Device); err != nil {
		t.Fatal(err)
	}
	if err := agg.Add(ctx, owner); !errors.Is(err, node.ErrPolicyMismatch) || !strings.Contains(err.Error(), `"part"`) {
		t.Fatalf("Add of an owner whose part is under other policies: %v", err)
	}

	// A node with policies takes devices built under the same ones only.
	m := surfaceNode(t, keepAll)
	if err := m.Aggregator().Add(ctx, light(t, "same", &surface.Policies{Custom: []surface.Policy{surfacetest.KeepAll()}})); err != nil {
		t.Fatal(err)
	}
	if err := m.Aggregator().Add(ctx, light(t, "none", nil)); !errors.Is(err, node.ErrPolicyMismatch) {
		t.Fatalf("Add of a zero-value device to a node with policies: %v", err)
	}
	if got := m.Surface().Policies; got != keepAll.Digest() {
		t.Fatal("the node reports another digest than its policies'")
	}
}

func TestNodeSurface(t *testing.T) {
	ctx := context.Background()
	n := surfaceNode(t, surface.Policies{})
	agg := n.Aggregator()
	owner, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("owner")})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AddPart(light(t, "part", nil).Device); err != nil {
		t.Fatal(err)
	}
	for _, ep := range []node.Endpoint{light(t, "light", nil), newPlug(t, "plug"), owner} {
		if err := agg.Add(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	rep := n.Surface()
	if rep.Policies != (surface.Policies{}).Digest() {
		t.Fatal("policies digest")
	}
	root := rep.Effective.Root
	if root.Key != "" || len(root.DeviceTypes) == 0 || len(root.Clusters) == 0 {
		t.Fatalf("root %+v", root)
	}
	keys := make([]string, 0, len(rep.Effective.Endpoints))
	byKey := map[string]surface.Endpoint{}
	for _, ep := range rep.Effective.Endpoints {
		keys = append(keys, ep.Key)
		byKey[ep.Key] = ep
	}
	if want := []string{node.AggregatorSurfaceKey, "light", "owner", "part", "plug"}; strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("endpoints %v, want %v", keys, want)
	}
	if a := byKey[node.AggregatorSurfaceKey]; len(a.DeviceTypes) == 0 || len(a.Clusters) == 0 {
		t.Fatalf("aggregator %+v", a)
	}
	// The host's own endpoint: its servers and device type, no decisions.
	if p := byKey["plug"]; len(p.Clusters) != 1 || p.Clusters[0].ID != 0x0006 || len(p.DeviceTypes) != 1 {
		t.Fatalf("plug %+v", p)
	}
	speaks := map[string]bool{}
	for _, d := range rep.Decisions {
		speaks[d.Endpoint] = true
	}
	if !speaks["light"] || !speaks["owner"] || !speaks["part"] || speaks["plug"] || !speaks[""] || speaks[node.AggregatorSurfaceKey] {
		t.Fatalf("decisions for %v", speaks)
	}
	if !json.Valid(mustJSON(t, rep)) {
		t.Fatal("report JSON")
	}

	// Started, the endpoints carry their numbers; the digest does not
	// see them. The plug is no device: nothing reports its number.
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	started := n.Surface()
	for _, ep := range started.Effective.Endpoints {
		if ep.Number == 0 && ep.Key != "plug" {
			t.Errorf("endpoint %q has no number once started", ep.Key)
		}
		if ep.Key == node.AggregatorSurfaceKey && ep.Number != 1 {
			t.Errorf("aggregator number %d", ep.Number)
		}
	}
	if started.Effective.Digest() != rep.Effective.Digest() {
		t.Fatal("starting the node changed the surface digest")
	}

	// The zero value's digest is the same on every node.
	if other := surfaceNode(t, surface.Policies{}); other.Surface().Policies != rep.Policies {
		t.Fatal("the zero value's digest differs between two nodes")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
