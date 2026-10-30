// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	closurecontroldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
)

// A withheld device has no endpoint, only its record in the surface
// report. Remove of its key drops the record and is no error — the key was
// declared — and Forget drops it too; a key never declared stays unknown.
func TestSurfaceWithheldRecordClearedByRemoveAndForget(t *testing.T) {
	low := surface.Policies{Level: "1.4.2"}
	closure := func() node.Endpoint {
		d, err := device.NewClosure(device.Options{StableKey: endpoint.StringKey("closure"), Name: "closure", Surface: &low},
			closurecontroldef.Declare{Features: closurecontroldef.FeaturePositioning})
		if d == nil {
			t.Fatalf("closure: %v", err)
		}
		return d
	}
	db := openDB(t, filepath.Join(t.TempDir(), "withheld-clear.db"))
	cfg := minimalConfig(db)
	cfg.Surface = low
	n := newNode(t, cfg)
	ctx := context.Background()
	if err := n.Aggregator().Add(ctx, light(t, "light", &low)); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.Stop(ctx) }()
	withheld := func() int { return len(n.Surface().Withheld) }

	if err := n.Aggregator().Add(ctx, closure()); !errors.Is(err, surface.ErrWithheld) || withheld() == 0 {
		t.Fatalf("add: %v, withheld %d", err, withheld())
	}
	if err := n.Aggregator().Remove(ctx, endpoint.StringKey("closure")); err != nil || withheld() != 0 {
		t.Fatalf("remove of the withheld key: %v, withheld %d", err, withheld())
	}
	if err := n.Aggregator().Remove(ctx, endpoint.StringKey("closure")); !errors.Is(err, node.ErrUnknownKey) {
		t.Fatalf("second remove: %v, want ErrUnknownKey", err)
	}
	if err := n.Aggregator().Add(ctx, closure()); !errors.Is(err, surface.ErrWithheld) || withheld() == 0 {
		t.Fatalf("add again: %v, withheld %d", err, withheld())
	}
	if err := n.Aggregator().Forget(ctx, endpoint.StringKey("closure")); err != nil || withheld() != 0 {
		t.Fatalf("forget of the withheld key: %v, withheld %d", err, withheld())
	}
}
