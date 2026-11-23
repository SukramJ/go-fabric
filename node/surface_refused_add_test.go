// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	closurecontroldef "thingfabric.dev/go-fabric/cluster/spec/closurecontrol"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/surface"
	"thingfabric.dev/go-fabric/surface/surfacetest"
)

// TestRefusedAddKeepsTheServedSurface (review II, R2-P6-store-2414): an
// Add of a second device under a served key that the specification level
// withholds is refused without touching the served endpoint: its persisted
// surface stays, and Node.Surface does not list the key as withheld beside
// it. A Replace of the served one by a withheld declaration is no refusal
// since decision 550 (#585): TestReplaceByAWithheldDeclaration.
func TestRefusedAddKeepsTheServedSurface(t *testing.T) {
	low := surface.Policies{Level: "1.4.2"}
	const key = "dup"
	closure := func() node.Endpoint {
		d, err := device.NewClosure(device.Options{StableKey: endpoint.StringKey(key), Name: key, Surface: low},
			closurecontroldef.Declare{Features: closurecontroldef.FeaturePositioning})
		if d == nil {
			t.Fatalf("closure: %v", err)
		}
		return d
	}
	db := openDB(t, filepath.Join(t.TempDir(), "refused-add.db"))
	cfg := minimalConfig(db)
	cfg.Surface = low
	n := newNode(t, cfg)
	ctx := context.Background()
	if err := n.Aggregator().Add(ctx, light(t, key, &low)); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	stored := func() string {
		t.Helper()
		v, _, err := store.New(db).GetSetting(ctx, "bridged_surface_digest."+key)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := stored()
	if before == "" {
		t.Fatal("no surface persisted for the served endpoint")
	}
	if err := n.Aggregator().Add(ctx, closure()); !errors.Is(err, node.ErrDuplicateKey) && !errors.Is(err, surface.ErrWithheld) {
		t.Fatalf("Add under a served key: %v, want a refusal", err)
	}
	if got := stored(); got != before {
		t.Errorf("persisted surface %q after the refused Add, want %q kept", got, before)
	}
	for _, w := range n.Surface().Withheld {
		if w.Endpoint == key {
			t.Errorf("Node.Surface lists the served key %q as withheld", key)
			break
		}
	}
}

// TestOTAProviderKeyIsReserved (review II, R2-P6-api-2415): the stable
// key of the node's own OTA provider endpoint is refused for a host's
// device, as the Aggregator's surface key is.
func TestOTAProviderKeyIsReserved(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "ota-key.db"))))
	err := n.Aggregator().Add(context.Background(), light(t, "go-fabric:ota-provider", nil))
	if !errors.Is(err, node.ErrReservedKey) {
		t.Fatalf("Add under the OTA provider's key: %v, want ErrReservedKey", err)
	}
}

// TestReplaceByAWithheldDeclaration (decision 550, #585): Replace of a
// served bridged endpoint by a declaration the specification level
// withholds is one step: the key is recorded as withheld in Node.Surface,
// served no more, and keeps its endpoint number — an Add of a declaration
// the level serves gets it back. A replacement that fails validation
// leaves the served endpoint in place, at its number.
func TestReplaceByAWithheldDeclaration(t *testing.T) {
	low := surface.Policies{Level: "1.4.2"}
	keepAll := surface.Policies{Level: "1.4.2", Custom: []surface.Policy{surfacetest.KeepAll()}}
	const key = "k"
	closure := func(k string) node.Endpoint {
		d, err := device.NewClosure(device.Options{StableKey: endpoint.StringKey(k), Name: k, Surface: low},
			closurecontroldef.Declare{Features: closurecontroldef.FeaturePositioning})
		if d == nil {
			t.Fatalf("closure: %v", err)
		}
		return d
	}
	db := openDB(t, filepath.Join(t.TempDir(), "replace-withheld.db"))
	cfg := minimalConfig(db)
	cfg.Surface = low
	n := newNode(t, cfg)
	ctx := context.Background()
	agg := n.Aggregator()
	if err := agg.Add(ctx, light(t, key, &low)); err != nil {
		t.Fatal(err)
	}
	if err := agg.Add(ctx, light(t, "neighbour", &low)); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.Stop(ctx) }()
	number, ok := servedNumberOf(n, key)
	if !ok {
		t.Fatal("the key is not served after Start")
	}
	withheld := func() bool {
		for _, w := range n.Surface().Withheld {
			if w.Endpoint == key {
				return true
			}
		}
		return false
	}

	// An invalid replacement changes nothing.
	if err := agg.Replace(ctx, endpoint.StringKey(key), light(t, "other", &low)); !errors.Is(err, node.ErrKeyMismatch) {
		t.Fatalf("Replace under another key: %v, want ErrKeyMismatch", err)
	}
	if err := agg.Replace(ctx, endpoint.StringKey(key), light(t, key, &keepAll)); !errors.Is(err, node.ErrPolicyMismatch) {
		t.Fatalf("Replace under other policies: %v, want ErrPolicyMismatch", err)
	}
	if got, ok := servedNumberOf(n, key); !ok || got != number || withheld() {
		t.Fatalf("after the invalid replacements: served %v at %d (want %d), withheld %v", ok, got, number, withheld())
	}

	// A withheld replacement records the withheld state, keeps the number.
	if err := agg.Replace(ctx, endpoint.StringKey(key), closure(key)); !errors.Is(err, surface.ErrWithheld) {
		t.Fatalf("Replace by a withheld declaration: %v, want ErrWithheld", err)
	}
	if !withheld() {
		t.Error("Node.Surface does not list the key as withheld after the Replace")
	}
	if got, ok := servedNumberOf(n, key); ok {
		t.Errorf("the withheld key is still served at %d", got)
	}
	if got := len(agg.Endpoints()); got != 1 {
		t.Errorf("the set holds %d endpoints after the withheld Replace, want 1 (the neighbour)", got)
	}
	if err := agg.Add(ctx, light(t, key, &low)); err != nil {
		t.Fatal(err)
	}
	if got, ok := servedNumberOf(n, key); !ok || got != number {
		t.Errorf("served again %v at %d, want the kept number %d", ok, got, number)
	}
	if withheld() {
		t.Error("the key is still listed as withheld once it is served again")
	}
}
