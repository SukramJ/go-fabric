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
)

// TestRefusedAddKeepsTheServedSurface (review II, R2-P6-store-2414): an
// Add of a second device under a served key, or a Replace of the served
// one, that the specification level withholds is refused without
// touching the served endpoint: its persisted surface stays, and Node.Surface
// does not list the key as withheld beside it.
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
	if err := n.Aggregator().Replace(ctx, endpoint.StringKey(key), closure()); !errors.Is(err, surface.ErrWithheld) {
		t.Fatalf("Replace by a withheld device: %v, want ErrWithheld", err)
	}
	if got := stored(); got != before {
		t.Errorf("persisted surface %q after the refused calls, want %q kept", got, before)
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
